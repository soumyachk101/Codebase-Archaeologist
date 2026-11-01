//! Tree-sitter extractor for Go.
//!
//! Reports syntax facts only: the package clause, functions/methods/structs/
//! interfaces/globals with exact ranges, import statements exactly as
//! written, and call sites of qualified identifiers (`x.Y(...)`). The Go
//! CLI resolves import paths to target packages and call targets to
//! concrete symbols; unresolvable references stay unresolved.

use tree_sitter::{Node, Parser};

use super::manifest::{
    symbol_id, symbol_key, CallFact, FileAnalysis, ImportBinding, ImportFact, Symbol,
};

/// Safety cap on call sites recorded per file.
const MAX_CALLS_PER_FILE: usize = 4_000;

pub fn extract(content: &str, path: &str) -> FileAnalysis {
    let lang = "go";
    let mut fa = FileAnalysis::new(path, lang);

    let mut parser = Parser::new();
    if parser.set_language(&tree_sitter_go::LANGUAGE.into()).is_err() {
        fa.parse_error = Some("parser language initialization failed".into());
        return fa;
    }

    let tree = match parser.parse(content, None) {
        Some(t) => t,
        None => {
            fa.parse_error = Some("parse failed".into());
            return fa;
        }
    };

    let root = tree.root_node();
    fa.parse_ok = !root.has_error();
    if root.has_error() {
        fa.parse_error = Some("tree contains ERROR nodes (partial parse)".into());
    }

    // Package clause.
    let pkg_name = go_package_name(root, content).unwrap_or_default();
    fa.package = pkg_name.clone();

    let module_id = symbol_id(lang, path, "package", &pkg_name);
    fa.symbols = vec![Symbol {
        id: module_id.clone(),
        kind: "package".into(),
        name: pkg_name.clone(),
        qualified_name: pkg_name.clone(),
        language: lang.into(),
        file: path.into(),
        start_line: 1,
        end_line: 1,
        start_byte: 0,
        end_byte: 0,
        exported: true,
        is_test: path.ends_with("_test.go"),
        parent: None,
        key: symbol_key(lang, path, "package", &pkg_name),
    }];

    // Declarations.
    for c in root.named_children() {
        match c.kind() {
            "function_declaration" => {
                let name = c.child_by_field_name("name").and_then(|n| text(content, n)).unwrap_or_default();
                if name.is_empty() {
                    continue;
                }
                let qn = name.clone();
                fa.symbols.push(Symbol {
                    id: symbol_id(lang, path, "function", &qn),
                    kind: "function".into(),
                    name: name.clone(),
                    qualified_name: qn,
                    language: lang.into(),
                    file: path.into(),
                    start_line: c.start_position().row + 1,
                    end_line: c.end_position().row + 1,
                    start_byte: c.start_byte() as u32,
                    end_byte: c.end_byte() as u32,
                    exported: is_exported(&name),
                    is_test: path.ends_with("_test.go"),
                    parent: None,
                    key: symbol_key(lang, path, "function", &qn),
                });
            }
            "method_declaration" => {
                let name = c.child_by_field_name("name").and_then(|n| text(content, n)).unwrap_or_default();
                if name.is_empty() {
                    continue;
                }
                let rec = c
                    .child_by_field_name("receiver")
                    .and_then(|r| r.named_children().next())
                    .and_then(|n| param_type_name(n, content))
                    .unwrap_or_default();
                let qn = if rec.is_empty() {
                    name.clone()
                } else {
                    format!("{rec}.{name}")
                };
                fa.symbols.push(Symbol {
                    id: symbol_id(lang, path, "method", &qn),
                    kind: "method".into(),
                    name: name.clone(),
                    qualified_name: qn,
                    language: lang.into(),
                    file: path.into(),
                    start_line: c.start_position().row + 1,
                    end_line: c.end_position().row + 1,
                    start_byte: c.start_byte() as u32,
                    end_byte: c.end_byte() as u32,
                    exported: is_exported(&name),
                    is_test: path.ends_with("_test.go"),
                    parent: None,
                    key: symbol_key(lang, path, "method", &qn),
                });
            }
            "type_declaration" => {
                for td in c.named_children().filter(|n| n.kind() == "type_spec") {
                    let name = td.child_by_field_name("name").and_then(|n| text(content, n)).unwrap_or_default();
                    if name.is_empty() {
                        continue;
                    }
                    let kind = if td.named_children().any(|n| n.kind() == "interface_type") {
                        "interface"
                    } else {
                        "struct"
                    };
                    fa.symbols.push(Symbol {
                        id: symbol_id(lang, path, kind, &name),
                        kind: kind.into(),
                        name: name.clone(),
                        qualified_name: name.clone(),
                        language: lang.into(),
                        file: path.into(),
                        start_line: td.start_position().row + 1,
                        end_line: td.end_position().row + 1,
                        start_byte: td.start_byte() as u32,
                        end_byte: td.end_byte() as u32,
                        exported: is_exported(&name),
                        is_test: path.ends_with("_test.go"),
                        parent: None,
                        key: symbol_key(lang, path, kind, &name),
                    });
                }
            }
            "const_declaration" | "var_declaration" => {
                for spec in c.named_children().filter(|n| n.kind() == "const_spec" || n.kind() == "var_spec") {
                    let names = spec.named_children().filter(|n| n.kind() == "identifier").map(|n| text(content, n).unwrap_or_default()).filter(|n| !n.is_empty()).collect::<Vec<_>>();
                    let kind = if c.kind() == "const_declaration" { "constant" } else { "variable" };
                    for nm in names {
                        fa.symbols.push(Symbol {
                            id: symbol_id(lang, path, kind, &nm),
                            kind: kind.into(),
                            name: nm.clone(),
                            qualified_name: nm.clone(),
                            language: lang.into(),
                            file: path.into(),
                            start_line: spec.start_position().row + 1,
                            end_line: spec.end_position().row + 1,
                            start_byte: spec.start_byte() as u32,
                            end_byte: spec.end_byte() as u32,
                            exported: is_exported(&nm),
                            is_test: path.ends_with("_test.go"),
                            parent: None,
                            key: symbol_key(lang, path, kind, &nm),
                        });
                    }
                }
            }
            _ => {}
        }
    }

    // Imports.
    if let Some(imps) = root.child_by_field_name("imports") {
        for c in imps.named_children() {
            if c.kind() != "import_declaration" {
                continue;
            }
            record_go_imports(c, content, &mut fa);
        }
    }

    // Call sites: qualified selector expressions `x.Y(...)`.
    let pkg_local = pkg_name.clone();
    let mut calls: Vec<CallFact> = Vec::new();
    visit_calls(root, content, &module_id, &pkg_local, &mut calls, &[], 0);
    fa.calls = calls;

    // Exported surface = capitalized identifiers.
    fa.exported_names = fa
        .symbols
        .iter()
        .filter(|s| s.kind != "package" && s.exported)
        .map(|s| s.name.clone())
        .collect();
    fa.exported_names.sort();
    fa.exported_names.dedup();

    fa
}

fn go_package_name(root: &Node, content: &str) -> Option<String> {
    for c in root.named_children() {
        if c.kind() == "package_clause" {
            return c
                .named_children()
                .find(|n| n.kind() == "identifier")
                .and_then(|n| text(content, n));
        }
    }
    None
}

fn record_go_imports(node: &Node, content: &str, fa: &mut FileAnalysis) {
    // Single-line forms: import "pkg" | import alias "pkg" | import . "pkg"
    // Block form: import ( ... ) with one import_specification per line.
    for c in node.named_children() {
        match c.kind() {
            "import_specification" => {
                record_single_go_import(c, content, fa, c.start_position().row + 1);
            }
            "string" => {
                let spec = text(content, c)
                    .map(|s| s.trim_matches('"').to_string())
                    .filter(|s| !s.is_empty());
                if let Some(spec) = spec {
                    fa.imports.push(ImportFact {
                        spec: spec.clone(),
                        line: c.start_position().row + 1,
                        dynamic: false,
                        bindings: vec![ImportBinding {
                            local: package_last_segment(&spec),
                            original: String::new(),
                        }],
                    });
                }
            }
            _ => {
                // import_list: children are import_specification nodes.
                for s in c.named_children() {
                    if s.kind() == "import_specification" {
                        record_single_go_import(s, content, fa, s.start_position().row + 1);
                    }
                }
            }
        }
    }
}

fn record_single_go_import(s: &Node, content: &str, fa: &mut FileAnalysis, line: u32) {
    let spec = s
        .named_children()
        .find(|n| n.kind() == "string")
        .and_then(|n| text(content, n))
        .map(|s| s.trim_matches('"').to_string());
    if let Some(spec) = spec {
        let alias = s
            .named_children()
            .find(|n| n.kind() == "identifier" || n.kind() == "period")
            .and_then(|n| text(content, n));
        let local = match alias {
            Some(a) if a != "." => a,
            _ => package_last_segment(&spec),
        };
        fa.imports.push(ImportFact {
            spec,
            line,
            dynamic: false,
            bindings: vec![ImportBinding { local, original: String::new() }],
        });
    }
}

fn package_last_segment(spec: &str) -> String {
    spec.rsplit('/').next().unwrap_or(spec).to_string()
}

fn is_exported(name: &str) -> bool {
    let mut chars = name.chars();
    matches!(chars.next(), Some(c) if c.is_uppercase())
}

fn param_type_name(node: &Node, content: &str) -> Option<String> {
    if let Some(t) = node.child_by_field_name("type") {
        return text(content, &t);
    }
    // parenthesized parameter groups: (*Recv)(...)
    node.named_children()
        .find(|n| n.kind() == "pointer_type" || n.kind() == "identifier" || n.kind() == "type_identifier")
        .and_then(|n| text(content, &n))
}

// ---------------------------------------------------------------------------
// Call sites
// ---------------------------------------------------------------------------

fn visit_calls(
    node: &Node,
    content: &str,
    module_id: &str,
    pkg: &str,
    out: &mut Vec<CallFact>,
    enclosing: &[String],
    depth: u32,
) {
    if depth > 64 || out.len() >= MAX_CALLS_PER_FILE {
        return;
    }
    for c in node.named_children() {
        match c.kind() {
            "function_declaration" | "method_declaration" => {
                let name = c.child_by_field_name("name").and_then(|n| text(content, n)).unwrap_or_default();
                if name.is_empty() {
                    continue;
                }
                let scope = if enclosing.is_empty() { name.clone() } else { format!("{}::{name}", enclosing.last().unwrap()) };
                let mut next = enclosing.to_vec();
                next.push(scope);
                visit_calls(c, content, module_id, pkg, out, &next, depth + 1);
            }
            "selector_expression" => {
                // Record only selector calls: parent must be a call_expression
                // and the selector must be its "function" field (x.Y(...)).
                if let Some(p) = c.parent() {
                    if p.kind() != "call_expression" || p.child_by_field_name("function") != Some(c) {
                        return;
                    }
                    let line = p.start_position().row + 1;
                    let target = text(content, c).unwrap_or_default();
                    if target.is_empty() {
                        return;
                    }
                    let mut parts = target.splitn(2, '.');
                    let base = parts.next().unwrap_or("").to_string();
                    let rest = parts.next().unwrap_or_default();
                    // `pkg.Local(...)` where pkg is this file's own package
                    // only occurs when importing the package itself (rare);
                    // when base == current package, the call is unqualified
                    // local usage — not an import-qualified call site.
                    if base == pkg || base.is_empty() {
                        return;
                    }
                    let imported_name = if rest.is_empty() {
                        None
                    } else {
                        Some(target)
                    };
                    let from = if enclosing.is_empty() {
                        String::new()
                    } else {
                        enclosing.last().unwrap().clone()
                    };
                    out.push(CallFact { line, from, target, imported_name });
                }
            }
            "call_expression" => {
                // unqualified identifier calls: record with target as the
                // identifier, so the resolver can look up local symbols.
                if let Some(f) = c.child_by_field_name("function") {
                    if f.kind() == "identifier" {
                        if let Some(t) = text(content, f) {
                            let line = f.start_position().row + 1;
                            let from = if enclosing.is_empty() {
                                String::new()
                            } else {
                                enclosing.last().unwrap().clone()
                            };
                            out.push(CallFact { line, from, target: t, imported_name: None });
                        }
                    }
                }
                visit_calls(c, content, module_id, pkg, out, enclosing, depth + 1);
            }
            "package_clause" | "import_declaration" | "go_statement" | "defer_statement" | "return_statement"
            | "expression_statement" => {
                visit_calls(c, content, module_id, pkg, out, enclosing, depth + 1);
            }
            _ => {
                if c.has_named_children() {
                    visit_calls(c, content, module_id, pkg, out, enclosing, depth + 1);
                }
            }
        }
    }
    let _ = module_id;
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

fn text<'a>(content: &'a str, node: &Node) -> Option<String> {
    node.utf8_text(content).map(str::to_string)
}

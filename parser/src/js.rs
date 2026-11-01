//! Tree-sitter extractors for JavaScript and TypeScript.
//!
//! The extractor reports **syntax facts only**:
//! - declarations (functions, classes, methods, variables, interfaces,
//!   types) with exact ranges from the parse
//! - import statements / dynamic imports exactly as written, with bindings
//! - call sites: the target expression as written, the 1-based line, and
//!   the enclosing top-level declared function when inside one
//!
//! Resolving import specifiers to target files and call targets to symbols
//! is done by the Go CLI, which knows the repository layout. This module
//! never invents a target.

use std::collections::BTreeSet;

use tree_sitter::{Node, Parser};

use super::manifest::{
    symbol_id, symbol_key, CallFact, FileAnalysis, ImportBinding, ImportFact, Symbol,
};

/// Safety cap on call sites recorded per file.
const MAX_CALLS_PER_FILE: usize = 4_000;

pub fn extract(content: &str, path: &str, ts: bool) -> FileAnalysis {
    let lang = if ts { "typescript" } else { "javascript" };
    let mut fa = FileAnalysis::new(path, lang);

    let mut parser = Parser::new();
    if (if ts {
        parser.set_language(&tree_sitter_typescript::LANGUAGE_TYPESCRIPT.into())
    } else {
        parser.set_language(&tree_sitter_javascript::LANGUAGE.into())
    })
    .is_err()
    {
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

    let module_id = symbol_id(lang, path, "module", path);
    let mut aliases: BTreeSet<(String, String)> = BTreeSet::new();

    // Symbols.
    let mut syms: Vec<Symbol> = Vec::new();
    add_module_symbol(&mut syms, path, lang, &module_id);
    collect_symbols(root, content, path, lang, &[], &mut syms, ts);
    fa.symbols = syms;

    // Imports.
    record_imports(root, content, &mut fa, &mut aliases);

    // Call sites.
    let mut calls: Vec<CallFact> = Vec::new();
    visit_calls(root, content, &module_id, &aliases, &mut calls, &[], 0);
    fa.calls = calls;

    fa.exported_names = exported_names(&fa, root, content, ts);
    fa
}

fn add_module_symbol(syms: &mut Vec<Symbol>, path: &str, lang: &str, module_id: &str) {
    syms.push(Symbol {
        id: module_id.clone(),
        kind: "module".into(),
        name: path.rsplit('/').next().unwrap_or(path).to_string(),
        qualified_name: path.to_string(),
        language: lang.into(),
        file: path.into(),
        start_line: 1,
        end_line: 1,
        start_byte: 0,
        end_byte: 0,
        exported: false,
        is_test: false,
        parent: None,
        key: symbol_key(lang, path, "module", path),
    });
}

// ---------------------------------------------------------------------------
// Symbols
// ---------------------------------------------------------------------------

fn collect_symbols(
    node: &Node,
    content: &str,
    path: &str,
    lang: &str,
    enclosing: &[String],
    out: &mut Vec<Symbol>,
    ts: bool,
) {
    for c in node.named_children() {
        match c.kind() {
            "function_declaration" | "generator_function_declaration" => {
                let name = c
                    .child_by_field_name("name")
                    .and_then(|n| text(content, n))
                    .unwrap_or_default();
                if name.is_empty() {
                    continue;
                }
                let qn = join(enclosing, &name);
                let exported = c.parent().map(parent_is_exported).unwrap_or(false);
                let is_test = path_is_test(path) || name.contains("test");
                push_symbol(out, c, content, path, lang, "function", &name, &qn, exported, is_test, enclosing);
                with_scope(enclosing, &name, |scope| collect_symbols(c, content, path, lang, scope, out, ts));
            }
            "class_declaration" => {
                let name = c
                    .child_by_field_name("name")
                    .and_then(|n| text(content, n))
                    .unwrap_or_default();
                if name.is_empty() {
                    continue;
                }
                let qn = join(enclosing, &name);
                let exported = c.parent().map(parent_is_exported).unwrap_or(false);
                push_symbol(out, c, content, path, lang, "class", &name, &qn, exported, path_is_test(path), enclosing);
                for block in c.named_children().filter(|n| n.kind() == "statement_block") {
                    for m in block.named_children() {
                        if m.kind() != "method_definition" {
                            continue;
                        }
                        let mn = m
                            .child_by_field_name("name")
                            .and_then(|n| text(content, n))
                            .unwrap_or_default();
                        if mn.is_empty() {
                            continue;
                        }
                        let mq = format!("{qn}.{mn}");
                        let is_test = path_is_test(path) || mn.contains("test");
                        let single = vec![qn.clone()];
                        push_symbol(out, m, content, path, lang, "method", &mn, &mq, false, is_test, &single);
                        collect_symbols(m, content, path, lang, &[qn.clone()], out, ts);
                    }
                }
            }
            "variable_declaration" => {
                let is_const = c.child(0).map(|n| n.kind() == "const").unwrap_or(false);
                let exported = parent_is_exported(c.parent());
                for vd in c.named_children().filter(|n| n.kind() == "variable_declarator") {
                    let name = vd
                        .child_by_field_name("name")
                        .and_then(|n| text(content, n))
                        .unwrap_or_default();
                    if name.is_empty() {
                        continue;
                    }
                    let kind = if is_const { "constant" } else { "variable" };
                    let qn = join(enclosing, &name);
                    let is_test = path_is_test(path) || name.contains("test");
                    push_symbol(out, vd, content, path, lang, kind, &name, &qn, exported, is_test, enclosing);
                    if let Some(v) = vd.child_by_field_name("value") {
                        if v.kind() == "arrow_function" || v.kind() == "function" {
                            with_scope(enclosing, &name, |scope| {
                                collect_symbols(v, content, path, lang, scope, out, ts);
                            });
                        }
                    }
                }
            }
            "interface_declaration" | "type_alias_declaration" if ts => {
                let name = c
                    .child_by_field_name("name")
                    .and_then(|n| text(content, n))
                    .unwrap_or_default();
                if name.is_empty() {
                    continue;
                }
                let kind = if c.kind() == "interface_declaration" { "interface" } else { "type" };
                let qn = join(enclosing, &name);
                let exported = c.parent().map(parent_is_exported).unwrap_or(false);
                push_symbol(out, c, content, path, lang, kind, &name, &qn, exported, false, enclosing);
            }
            "export_statement" => {
                for d in c.named_children() {
                    if matches!(
                        d.kind(),
                        "function_declaration"
                            | "generator_function_declaration"
                            | "class_declaration"
                            | "variable_declaration"
                    ) {
                        collect_symbols(d, content, path, lang, enclosing, out, ts);
                    }
                }
            }
            "if_statement" | "for_statement" | "while_statement" | "switch_statement"
            | "statement_block" | "try_statement" => {
                collect_symbols(c, content, path, lang, enclosing, out, ts);
            }
            _ => {}
        }
    }
}

fn with_scope<T>(enclosing: &[String], name: &str, f: impl Fn(&[String]) -> T) -> T {
    let mut next = enclosing.to_vec();
    next.push(name.to_string());
    f(&next)
}

fn path_is_test(path: &str) -> bool {
    let base = path.rsplit('/').next().unwrap_or(path);
    base.contains(".test.") || base.contains(".spec.") || path.contains("__tests__")
}

fn parent_is_exported(parent: Option<&Node>) -> bool {
    parent.map(|p| p.kind() == "export_statement").unwrap_or(false)
}

fn join(enclosing: &[String], name: &str) -> String {
    if enclosing.is_empty() {
        name.to_string()
    } else {
        format!("{}|{}", enclosing.join("|"), name)
    }
}

fn push_symbol(
    out: &mut Vec<Symbol>,
    node: &Node,
    content: &str,
    path: &str,
    lang: &str,
    kind: &str,
    name: &str,
    qn: &str,
    exported: bool,
    is_test: bool,
    enclosing: &[String],
) {
    out.push(Symbol {
        id: symbol_id(lang, path, kind, qn),
        kind: kind.into(),
        name: name.into(),
        qualified_name: qn.into(),
        language: lang.into(),
        file: path.into(),
        start_line: node.start_position().row + 1,
        end_line: node.end_position().row + 1,
        start_byte: node.start_byte() as u32,
        end_byte: node.end_byte() as u32,
        exported,
        is_test,
        parent: enclosing.last().cloned(),
        key: symbol_key(lang, path, kind, qn),
    });
    let _ = content;
}

// ---------------------------------------------------------------------------
// Imports
// ---------------------------------------------------------------------------

fn record_imports(root: &Node, content: &str, fa: &mut FileAnalysis, aliases: &mut BTreeSet<(String, String)>) {
    for c in walk_all(root) {
        match c.kind() {
            "import_statement" => {
                let spec = import_specifier(content, c);
                let line = c.start_position().row + 1;
                let mut bindings: Vec<ImportBinding> = Vec::new();

                if let Some(d) = c.child_by_field_name("default").and_then(|n| text(content, n)) {
                    bindings.push(ImportBinding { local: d.clone(), original: String::new() });
                    aliases.insert((d.clone(), d));
                }
                if let Some(n) = c.child_by_field_name("namespace").and_then(|n| text(content, n)) {
                    bindings.push(ImportBinding { local: n.clone(), original: String::new() });
                    aliases.insert((n, n));
                }
                if let Some(cl) = c.child_by_field_name("import_clause") {
                    for s in cl.named_children() {
                        if s.kind() != "import_specifier" {
                            continue;
                        }
                        let orig = s.child_by_field_name("name").and_then(|n| text(content, n)).unwrap_or_default();
                        let alias = s.child_by_field_name("alias").and_then(|n| text(content, n)).unwrap_or_default();
                        if orig.is_empty() {
                            continue;
                        }
                        let local = if alias.is_empty() { orig.clone() } else { alias };
                        bindings.push(ImportBinding { local: local.clone(), original: orig });
                        aliases.insert((local, orig));
                    }
                }
                fa.imports.push(ImportFact { spec, line, dynamic: false, bindings });
            }
            "dynamic_import" => {
                let spec = c
                    .child_by_field_name("module")
                    .and_then(|m| text(content, m))
                    .unwrap_or_default();
                let line = c.start_position().row + 1;
                fa.imports.push(ImportFact {
                    spec,
                    line,
                    dynamic: true,
                    bindings: vec![ImportBinding { local: format!("dyn@{line}"), original: String::new() }],
                });
            }
            _ => {}
        }
    }
}

fn import_specifier(content: &str, node: &Node) -> String {
    node.child_by_field_name("source")
        .and_then(|s| text(content, s))
        .unwrap_or_default()
        .trim_matches(|ch| ch == '"' || ch == '\'' || ch == '`')
        .to_string()
}

// ---------------------------------------------------------------------------
// Call sites
// ---------------------------------------------------------------------------

fn visit_calls(
    node: &Node,
    content: &str,
    module_id: &str,
    aliases: &BTreeSet<(String, String)>,
    out: &mut Vec<CallFact>,
    enclosing: &[String],
    depth: u32,
) {
    if depth > 64 || out.len() >= MAX_CALLS_PER_FILE {
        return;
    }
    for c in node.named_children() {
        match c.kind() {
            "function_declaration" | "generator_function_declaration" => {
                let name = c
                    .child_by_field_name("name")
                    .and_then(|n| text(content, n))
                    .unwrap_or_default();
                if name.is_empty() {
                    continue;
                }
                with_scope(enclosing, &name, |scope| {
                    visit_calls(c, content, module_id, aliases, out, scope, depth + 1);
                });
            }
            "method_definition" => {
                let name = c
                    .child_by_field_name("name")
                    .and_then(|n| text(content, n))
                    .unwrap_or_default();
                let owner = enclosing.last().map(String::as_str).unwrap_or(module_id);
                let scope = if name.is_empty() {
                    format!("{owner}.anon@{}", c.start_position().row + 1)
                } else {
                    format!("{owner}.{name}")
                };
                let mut next = enclosing.to_vec();
                next.push(scope);
                visit_calls(c, content, module_id, aliases, out, &next, depth + 1);
            }
            "class_declaration" | "arrow_function" | "if_statement" | "for_statement"
            | "while_statement" | "switch_statement" | "statement_block" | "try_statement"
            | "variable_declaration" => {
                visit_calls(c, content, module_id, aliases, out, enclosing, depth + 1);
            }
            "call_expression" => {
                if let Some(f) = c.child_by_field_name("function") {
                    let line = f.start_position().row + 1;
                    let from = if enclosing.is_empty() {
                        String::new()
                    } else {
                        enclosing.last().unwrap().clone()
                    };
                    match f.kind() {
                        "identifier" => {
                            if let Some(t) = text(content, f) {
                                let imported = aliases.iter().find(|(l, _)| l == &t).map(|(_, o)| o.clone());
                                out.push(CallFact {
                                    line,
                                    from: from.clone(),
                                    target: t,
                                    imported_name: imported,
                                });
                            }
                        }
                        "member_expression" => {
                            let obj = f.child_by_field_name("object");
                            let member = f.child_by_field_name("property");
                            if let (Some(o), Some(m)) = (obj, member) {
                                let obj_t = text(content, o).unwrap_or_default();
                                let mem_t = text(content, m).unwrap_or_default();
                                if obj_t.is_empty() || mem_t.is_empty() {
                                    continue;
                                }
                                let target = format!("{obj_t}.{mem_t}");
                                let imported = aliases.iter().find(|(l, _)| l == &obj_t).map(|(_, x)| x.clone());
                                out.push(CallFact {
                                    line,
                                    from: from.clone(),
                                    target,
                                    imported_name: imported,
                                });
                            }
                        }
                        _ => {}
                    }
                }
                // arguments may contain nested calls
                visit_calls(c, content, module_id, aliases, out, enclosing, depth + 1);
            }
            _ => {
                if c.has_named_children() {
                    visit_calls(c, content, module_id, aliases, out, enclosing, depth + 1);
                }
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Exports (public surface of the module)
// ---------------------------------------------------------------------------

fn exported_names(fa: &FileAnalysis, root: &Node, content: &str, ts: bool) -> Vec<String> {
    let mut out: Vec<String> = Vec::new();

    for c in root.named_children() {
        if c.kind() != "export_statement" {
            continue;
        }
        for d in c.named_children() {
            if let Some(n) = decl_export_name(d) {
                out.push(n);
            }
        }
        if let Some(cl) = c.child_by_field_name("export_clause") {
            for s in cl.named_children() {
                match s.kind() {
                    "shorthand_identifier_export" => {
                        if let Some(n) = text(content, s.child_by_field_name("name").unwrap_or(s)) {
                            out.push(n);
                        }
                    }
                    "identifier_export" => {
                        if let Some(n) = s.child_by_field_name("alias").and_then(|n| text(content, n)) {
                            out.push(n);
                        } else if let Some(n) = s.child_by_field_name("name").and_then(|n| text(content, n)) {
                            out.push(n);
                        }
                    }
                    _ => {}
                }
            }
        }
        if c.child_by_field_name("default").is_some() {
            out.push("default".into());
        }
    }

    // CommonJS: module.exports.foo
    if content.contains("module.exports") {
        for c in walk_all(root) {
            if c.kind() != "member_expression" {
                continue;
            }
            let obj = c.child_by_field_name("object").and_then(|n| text(content, n));
            if obj.as_deref() != Some("module") {
                continue;
            }
            if let Some(prop) = c.child_by_field_name("property").and_then(|n| text(content, n)) {
                out.push(prop);
            }
        }
        // Top-level declarations in a CJS file are reachable too.
        for s in fa.symbols.iter() {
            if matches!(s.kind.as_str(), "function" | "class" | "method") {
                out.push(s.name.clone());
            }
        }
    }

    out.sort();
    out.dedup();
    let _ = ts;
    out
}

fn decl_export_name(node: &Node) -> Option<String> {
    match node.kind() {
        "function_declaration" | "generator_function_declaration" | "class_declaration" => node
            .child_by_field_name("name")
            .and_then(|n| n.utf8_text())
            .map(str::to_string),
        "variable_declaration" => node
            .named_children()
            .find(|d| d.kind() == "variable_declarator")
            .and_then(|d| d.child_by_field_name("name"))
            .and_then(|n| n.utf8_text())
            .map(str::to_string),
        _ => None,
    }
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

fn text<'a>(content: &'a str, node: &Node) -> Option<String> {
    node.utf8_text(content).map(str::to_string)
}

fn walk_all<'a>(node: &Node) -> impl Iterator<Item = Node<'a>> {
    fn gen<'a>(n: &Node<'a>, out: &mut Vec<Node<'a>>) {
        for c in n.named_children() {
            out.push(c);
            gen(c, out);
        }
    }
    let mut v = Vec::new();
    gen(node, &mut v);
    v.into_iter()
}

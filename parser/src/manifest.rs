//! Wire-format types shared between the parser binary and the Go CLI.
//!
//! Provenance rules encoded in this schema (see docs/evidence-provenance.md):
//! - every `Symbol` carries an exact file path, 1-based line range and byte
//!   range observed from a concrete parse of the provided source text;
//! - `ImportFact` and `CallFact` record reference sites with the file and
//!   line where they were observed. They carry a target *hint* (the source
//!   expression) and nothing more: which concrete symbol (if any) a
//!   reference points to is decided later by the Go CLI, which knows the
//!   repository's directory structure. Unresolvable references stay
//!   unresolved in the persisted graph.
//! - nothing in this schema is invented; every record maps 1:1 to source.

use std::collections::BTreeSet;

use hex::encode;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

pub const SCHEMA_VERSION: u32 = 1;
pub const PARSER_VERSION: &str = "archaeologist-parser/0.1.0";

/// Stable, line-independent identity of a symbol.
///
/// Symbol IDs are commit-scoped: they identify a symbol within a single
/// index run. The same logical symbol in a later commit gets a new ID
/// (file path may have moved) but the same `key`, which is what links
/// symbols across history.
pub fn symbol_id(lang: &str, path: &str, kind: &str, qualified: &str) -> String {
    let mut h = Sha256::new();
    h.update(b"archaeologist:sym:v1|");
    h.update(lang.as_bytes());
    h.update(b"|");
    h.update(path.as_bytes());
    h.update(b"|");
    h.update(kind.as_bytes());
    h.update(b"|");
    h.update(qualified.as_bytes());
    encode(h.finalize())
}

/// Canonical, line-independent identity used to link symbols across commits
/// (line numbers move when files change; qualified names do not).
pub fn symbol_key(lang: &str, path: &str, kind: &str, qualified: &str) -> String {
    format!("{lang}|{path}|{kind}|{qualified}")
}

/// Stable identity of a persisted edge, computed from the observable
/// evidence tuple only (kind, source location, hint). The Go CLI uses these
/// IDs verbatim from the parser output so the same fact keeps the same ID
/// across index runs while its evidence is unchanged.
pub fn edge_id(kind: &str, from_symbol: &str, hint: &str, source_file: &str, source_line: u32) -> String {
    let mut h = Sha256::new();
    h.update(b"archaeologist:edge:v1|");
    h.update(kind.as_bytes());
    h.update(b"|");
    h.update(from_symbol.as_bytes());
    h.update(b"|");
    h.update(hint.as_bytes());
    h.update(b"|");
    h.update(source_file.as_bytes());
    h.update(b"|");
    write!(h, "{source_line}").ok();
    encode(h.finalize())[..24].to_string()
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct Symbol {
    pub id: String,
    /// function | method | class | struct | interface | type | enum
    /// | variable | constant | module | package
    pub kind: String,
    pub name: String,
    pub qualified_name: String,
    pub language: String,
    pub file: String,
    /// 1-based line where the declaration starts.
    pub start_line: u32,
    pub end_line: u32,
    pub start_byte: u32,
    pub end_byte: u32,
    pub exported: bool,
    pub is_test: bool,
    /// Qualified name of the enclosing scope ("" when top-level).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub parent: Option<String>,
    /// Line-independent identity used for cross-commit history.
    pub key: String,
}

/// One import statement (or dynamic import expression) as observed.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct ImportFact {
    /// Module specifier exactly as written (`"./util"`, `"lodash"`).
    pub spec: String,
    /// 1-based line of the import statement.
    pub line: u32,
    /// True for `import("...")` / `require("...")` expression sites.
    pub dynamic: bool,
    /// Human-facing expression of the import site (evidence display).
    pub hint: String,
    /// `local -> original` bindings. `original` is "" for default and
    /// namespace bindings where no original name is observable.
    pub bindings: Vec<ImportBinding>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct ImportBinding {
    pub local: String,
    pub original: String,
}

/// One call-site observation in this file. The target is the source
/// expression as written; symbol-level resolution happens in the CLI.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct CallFact {
    /// 1-based line of the call.
    pub line: u32,
    /// Qualified name of the enclosing top-level declared function/method,
    /// empty string when the call executes at module load (top level).
    pub from: String,
    /// The target expression as written: `x`, `a.b`.
    pub target: String,
    /// Human-facing display form of the reference site.
    pub hint: String,
    /// When the target's base identifier is bound by an import in this
    /// file, the imported original name ("" otherwise). This is the best
    /// deterministic hint for resolving the call to a concrete symbol.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub imported_name: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
pub struct SkippedFile {
    pub path: String,
    pub reason: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct FileAnalysis {
    pub path: String,
    pub language: String,
    pub parse_ok: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub parse_error: Option<String>,
    /// Go: the package clause name for this file ("").
    #[serde(default)]
    pub package: String,
    pub symbols: Vec<Symbol>,
    pub imports: Vec<ImportFact>,
    pub calls: Vec<CallFact>,
    /// Names this file's module surfaces to importers (explicit exports,
    /// default export, or Go exported identifiers).
    pub exported_names: Vec<String>,
}

impl FileAnalysis {
    pub fn new(path: &str, language: &str) -> Self {
        Self {
            path: path.to_string(),
            language: language.to_string(),
            parse_ok: false,
            parse_error: None,
            package: String::new(),
            symbols: Vec::new(),
            imports: Vec::new(),
            calls: Vec::new(),
            exported_names: Vec::new(),
        }
    }

    pub fn failed(path: &str, language: &str, error: &str) -> Self {
        let mut fa = Self::new(path, language);
        fa.parse_error = Some(error.to_string());
        fa
    }

    /// The `module`/`package` symbol of this file, if present.
    pub fn module_symbol_id(&self) -> Option<&str> {
        self.symbols
            .iter()
            .find(|s| s.kind == "module" || s.kind == "package")
            .map(|s| s.id.as_str())
    }

    /// Attach stable fact-edge IDs to this file's import and call facts.
    /// IDs are deterministic over the evidence tuple, so the same fact
    /// keeps the same ID across index runs.
    pub fn attach_fact_ids(&mut self, module_id: &str) {
        for imp in &mut self.imports {
            imp.hint = build_import_hint(imp);
        }
        for c in &mut self.calls {
            c.hint = c.target.clone();
        }
        let _ = module_id;
    }

    /// Stable IDs of this file's fact edges (for storage/dedup).
    pub fn import_fact_ids(&self) -> Vec<String> {
        self.imports
            .iter()
            .map(|i| edge_id("IMPORTS", &self.path, &i.hint, &self.path, i.line))
            .collect()
    }

    pub fn call_fact_ids(&self) -> Vec<String> {
        self.calls
            .iter()
            .map(|c| edge_id("CALLS", &self.path, &c.hint, &self.path, c.line))
            .collect()
    }
}

/// Human-facing display of an import fact.
fn build_import_hint(f: &ImportFact) -> String {
    if f.dynamic {
        return format!("import(\"{}\")", f.spec);
    }
    let mut parts: Vec<String> = Vec::new();
    for b in &f.bindings {
        if b.local == "." || b.local.is_empty() {
            parts.push("*".into());
            continue;
        }
        if b.original.is_empty() || b.original == b.local {
            parts.push(b.local.clone());
        } else {
            parts.push(format!("{} as {}", b.original, b.local));
        }
    }
    if parts.is_empty() {
        format!("import \"{}\"", f.spec)
    } else {
        format!("import {} from \"{}\"", parts.join(", "), f.spec)
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Manifest {
    pub schema_version: u32,
    pub parser_version: String,
    /// Commit the snapshot was taken at, as recorded by the CLI.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub commit: Option<String>,
    pub languages: BTreeSet<String>,
    pub files: Vec<FileAnalysis>,
    pub skipped: Vec<SkippedFile>,
}

impl Manifest {
    pub fn new(commit: Option<String>, files: Vec<FileAnalysis>) -> Self {
        Self {
            schema_version: SCHEMA_VERSION,
            parser_version: PARSER_VERSION.to_string(),
            commit,
            languages: BTreeSet::new(),
            files,
            skipped: Vec::new(),
        }
    }

    /// Dedupe, sort, and derive deterministic metadata. Pure: the manifest
    /// is a fact report, and this step must not change any facts.
    pub fn finalize(&mut self) {
        for fa in &mut self.files {
            let mut seen: BTreeSet<String> = BTreeSet::new();
            fa.symbols.retain(|s| seen.insert(s.id.clone()));
            fa.symbols.sort_by(|a, b| a.id.cmp(&b.id));
            fa.imports.sort_by(|a, b| a.line.cmp(&b.line).then(a.spec.cmp(&b.spec)));
            fa.calls.sort_by(|a, b| a.line.cmp(&b.line).then(a.from.cmp(&b.from).then(a.target.cmp(&b.target))));
            fa.exported_names.sort();
            fa.exported_names.dedup();
            self.languages.insert(fa.language.clone());
        }
        self.files.sort_by(|a, b| a.path.cmp(&b.path));
        self.skipped.sort_by(|a, b| a.path.cmp(&b.path));
    }
}

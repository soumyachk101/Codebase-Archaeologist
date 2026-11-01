pub mod go;
pub mod js;
pub mod language;
pub mod manifest;
pub mod resolve;

use std::collections::HashMap;

use anyhow::Result;
use manifest::Manifest;
use serde::Deserialize;

/// Schema version of the JSON wire format emitted by the parser.
pub const SCHEMA_VERSION: u32 = manifest::SCHEMA_VERSION;

/// Bounded file size: files larger than this are skipped, not parsed.
/// This keeps the tree-sitter process bounded on generated/vendored sources.
pub const MAX_FILE_BYTES: usize = 1_000_000;

/// The JSON document the Go CLI sends on stdin.
#[derive(Debug, Deserialize)]
pub struct InputManifest {
    /// Commit SHA the source snapshot belongs to (informational; provenance
    /// is anchored by the CLI/database, not by this field).
    #[serde(default)]
    pub commit: Option<String>,
    /// "auto" (default) or one of: javascript, typescript, go.
    /// When a language is pinned, files whose extension does not match are
    /// recorded as skipped rather than guessed.
    #[serde(default = "default_language")]
    pub language: String,
    /// Repository-root-relative paths; contents are source text.
    pub files: Vec<InputFile>,
}

fn default_language() -> String {
    "auto".to_string()
}

#[derive(Debug, Deserialize)]
pub struct InputFile {
    pub path: String,
    pub content: String,
}

/// Parse every supported file, resolve references across the batch, and
/// produce a deterministic manifest.
///
/// Guarantees (enforced in tests):
/// - every symbol carries an exact file + line range from a concrete parse
///   of the provided source;
/// - every edge cites the file and line where the reference was observed;
/// - unresolvable references are reported as unresolved, never invented.
pub fn run(input: &InputManifest) -> Result<Manifest> {
    let mut analyses: HashMap<String, manifest::FileAnalysis> = HashMap::new();
    let mut skipped: Vec<manifest::SkippedFile> = Vec::new();

    for file in &input.files {
        let lang = language::detect(&file.path, &input.language);
        match lang {
            Some(language::Lang::JavaScript) => {
                let fa = js::extract(&file.content, &file.path, false);
                analyses.insert(file.path.clone(), fa);
            }
            Some(language::Lang::TypeScript) => {
                let fa = js::extract(&file.content, &file.path, true);
                analyses.insert(file.path.clone(), fa);
            }
            Some(language::Lang::Go) => {
                let fa = go::extract(&file.content, &file.path);
                analyses.insert(file.path.clone(), fa);
            }
            None => {
                skipped.push(manifest::SkippedFile {
                    path: file.path.clone(),
                    reason: "unsupported file extension for parser languages (javascript, typescript, go)".to_string(),
                });
            }
        }
    }

    let mut files: Vec<manifest::FileAnalysis> = analyses.into_values().collect();
    files.sort_by(|a, b| a.path.cmp(&b.path));

    let mut m = manifest::Manifest::new(input.commit.clone(), files);
    m.skipped = skipped;
    m.finalize();
    Ok(m)
}

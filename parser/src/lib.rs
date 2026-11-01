pub mod go;
pub mod js;
pub mod language;
pub mod manifest;

use std::collections::HashMap;

use anyhow::Result;
use manifest::Manifest;
use serde::Deserialize;

/// Schema version of the JSON wire format emitted by the parser.
pub const SCHEMA_VERSION: u32 = manifest::SCHEMA_VERSION;

/// Bounded file size: files larger than this are recorded as skipped, not
/// parsed, so the tree-sitter process stays bounded on generated sources.
pub const MAX_FILE_BYTES: usize = 1_000_000;

/// The JSON document the Go CLI sends on stdin.
#[derive(Debug, Deserialize)]
pub struct InputManifest {
    /// Commit SHA the source snapshot belongs to (informational; provenance
    /// is anchored by the CLI and database, not by this field).
    #[serde(default)]
    pub commit: Option<String>,
    /// "auto" (default) or one of: javascript, typescript, go. When a
    /// language is pinned, files whose extension does not match are recorded
    /// as skipped rather than guessed.
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

/// Parse every supported file and produce a deterministic manifest.
///
/// Guarantees (enforced in tests):
/// - every symbol carries an exact file + line/byte range from a concrete
///   parse of the provided source;
/// - every import fact and call fact cites the file and line where it was
///   observed;
/// - nothing is invented: the manifest contains no target symbol, only
///   source-level evidence.
pub fn run(input: &InputManifest) -> Result<Manifest> {
    let mut analyses: HashMap<String, manifest::FileAnalysis> = HashMap::new();
    let mut skipped: Vec<manifest::SkippedFile> = Vec::new();

    for file in &input.files {
        if file.content.len() > MAX_FILE_BYTES {
            skipped.push(manifest::SkippedFile {
                path: file.path.clone(),
                reason: "file exceeds parser size bound".into(),
            });
            continue;
        }
        match language::detect(&file.path, &input.language) {
            Some(language::Lang::JavaScript) => {
                analyses.insert(file.path.clone(), js::extract(&file.content, &file.path, false));
            }
            Some(language::Lang::TypeScript) => {
                analyses.insert(file.path.clone(), js::extract(&file.content, &file.path, true));
            }
            Some(language::Lang::Go) => {
                analyses.insert(file.path.clone(), go::extract(&file.content, &file.path));
            }
            None => {
                skipped.push(manifest::SkippedFile {
                    path: file.path.clone(),
                    reason: format!(
                        "unsupported file extension for pinned language '{input.language}'"
                    ),
                });
            }
        }
    }

    let mut files: Vec<manifest::FileAnalysis> = analyses.into_values().collect();
    let mut m = manifest::Manifest::new(input.commit.clone(), files);
    m.skipped = skipped;
    m.finalize();
    Ok(m)
}

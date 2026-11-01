//! Language detection from file paths, honouring an optional pin.

use std::path::Path;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Lang {
    JavaScript,
    TypeScript,
    Go,
}

impl Lang {
    pub fn name(self) -> &'static str {
        match self {
            Lang::JavaScript => "javascript",
            Lang::TypeScript => "typescript",
            Lang::Go => "go",
        }
    }
}

fn ext_of(path: &str) -> &str {
    Path::new(path)
        .extension()
        .and_then(|e| e.to_str())
        .unwrap_or("")
}

/// Detect the language of `path`. When `pin` is anything other than
/// "auto", a mismatch is reported as unsupported instead of guessed.
pub fn detect(path: &str, pin: &str) -> Option<Lang> {
    let lang = match ext_of(path) {
        "js" | "mjs" | "cjs" => Lang::JavaScript,
        "ts" | "tsx" => Lang::TypeScript,
        "go" => Lang::Go,
        _ => return None,
    };
    if pin == "auto" {
        Some(lang)
    } else if pin == lang.name() {
        Some(lang)
    } else {
        None
    }
}

pub const JS_EXTENSIONS: [&str; 3] = ["js", "mjs", "cjs"];
pub const TS_EXTENSIONS: [&str; 2] = ["ts", "tsx"];
pub const GO_EXTENSIONS: [&str; 1] = ["go"];

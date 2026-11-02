//! archaeologist-parser binary.
//!
//! Reads an input manifest (JSON) from stdin:
//!   { "commit": "...", "language": "auto", "files": [{ "path", "content" }] }
//! and writes a parsed manifest to stdout:
//!   { "schemaVersion", "parserVersion", "commit", "languages", "files": [
//!       { "path", "language", "parseOk", "symbols": [...],
//!         "imports": [...], "calls": [...], "exportedNames": [...] }],
//!     "skipped": [...] }
//!
//! The Go CLI treats this document as a fact report: it stores every record
//! and resolves targets itself.

use std::io::{self, Read};
use std::process::ExitCode;

use parser::manifest::Manifest;
use parser::InputManifest;

fn main() -> ExitCode {
    let mut raw = String::new();
    match io::stdin().read_to_string(&mut raw) {
        Ok(n) if n == 0 => {
            eprintln!("archaeologist-parser: empty stdin; nothing to parse");
            return ExitCode::FAILURE;
        }
        Ok(_) => {}
        Err(e) => {
            eprintln!("archaeologist-parser: reading stdin failed: {e}");
            return ExitCode::FAILURE;
        }
    }

    let input: InputManifest = match serde_json::from_str(&raw) {
        Ok(i) => i,
        Err(e) => {
            eprintln!("archaeologist-parser: input manifest invalid: {e}");
            return ExitCode::FAILURE;
        }
    };

    let manifest: Manifest = match parser::run(&input) {
        Ok(m) => m,
        Err(e) => {
            eprintln!("archaeologist-parser: {e:#}");
            return ExitCode::FAILURE;
        }
    };

    let mut out = match serde_json::to_string_pretty(&manifest) {
        Ok(s) => s,
        Err(e) => {
            eprintln!("archaeologist-parser: serialization failed: {e}");
            return ExitCode::FAILURE;
        }
    };
    out.push('\n');
    print!("{out}");
    ExitCode::SUCCESS
}

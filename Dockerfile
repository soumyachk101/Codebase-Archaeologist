# Codebase Archaeologist — multi-stage production image.
#
# Fixed layout (documented for operators):
#   /opt/archaeologist/bin/archaeologist         Go CLI + store layer (SQLite/Postgres dialects)
#   /opt/archaeologist/bin/archaeologist-parser Rust tree-sitter extractor
#   /var/lib/archaeologist                       default ARCH_DATA_DIR (sqlite file + git snapshots)
#
# The Go CLI locates the parser as a separate process via ARCH_PARSER_BIN
# (default "archaeologist-parser" on $PATH): JSON input on stdin, JSON output
# on stdout (see internal/ingest/pipeline.go runParser). Both binaries live on
# PATH here so the default works with zero env vars.
#
# NOTE: this image ships the indexer + CLI + store layer. The API entrypoint
# (Chi service under service/) lands with service completion — service/cmd
# is currently empty.

# ---------------------------------------------------------------------------
# Stage 1: Rust parser binary
# parser/Cargo.toml: edition 2021; tree-sitter 0.22 + grammars go 0.20,
# javascript 0.22, typescript 0.21. No pinned rustc version and no feature
# flags in Cargo.toml; the grammars ship their C bindings in-crate and
# compile via the cc driver (gcc present in the slim image).
# tree-sitter-typescript builds two grammars (TS + TSX).
# ---------------------------------------------------------------------------
FROM rust:1.84-slim AS parser-builder
WORKDIR /src
COPY parser/ parser/
RUN cargo build --release --manifest-path parser/Cargo.toml

# ---------------------------------------------------------------------------
# Stage 2: Go binary. go.mod requires go 1.23. modernc.org/sqlite is a
# pure-Go driver, so CGO_ENABLED=0: no cgo, no libc dependency in the
# resulting binary.
# ---------------------------------------------------------------------------
FROM golang:1.23 AS go-builder
WORKDIR /src
COPY go.mod go.sum* ./
COPY cli/ cli/
COPY internal/ internal/
RUN CGO_ENABLED=0 GOFLAGS=-mod=mod go build -trimpath -o /out/archaeologist ./cli/cmd

# ---------------------------------------------------------------------------
# Final: both binaries under one documented directory. git is required at
# runtime — internal/ingest drives `git archive`, `git log`, `git
# rev-parse` against the target working tree; tar ships with the base.
# ---------------------------------------------------------------------------
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends git ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --user-group --home-dir /var/lib/archaeologist archaeologist

COPY --from=parser-builder /src/parser/target/release/parser /opt/archaeologist/bin/archaeologist-parser
COPY --from=go-builder /out/archaeologist /opt/archaeologist/bin/archaeologist

# Env wiring matches internal/envx/envx.go exactly:
#   ARCH_PARSER_BIN default "archaeologist-parser" -> satisfied by PATH below
#   ARCH_DATA_DIR   default "./data"               -> pinned to a persistent dir
ENV PATH=/opt/archaeologist/bin:${PATH} \
    ARCH_DATA_DIR=/var/lib/archaeologist

USER archaeologist
WORKDIR /var/lib/archaeologist
VOLUME /var/lib/archaeologist

LABEL org.opencontainers.image.title="archaeologist" \
      org.opencontainers.image.description="Codebase Archaeologist: tree-sitter indexer (Rust) + Go CLI + store layer. API entrypoint lands with service completion."

# Cobra root: bare "archaeologist" prints help. Subcommands:
#   archaeologist init | index | status | ... (see internal/cli)
CMD ["archaeologist"]

// Sample Rust fixture for the source content-type family.
// Mirrors sample.go's structure for cross-language comparison.

/// Documentation comment — Rust uses `///` and `//!`. Both start
/// with `//` so our line-comment classifier handles them.

fn greet(name: &str) {
    println!("hello, {}", name);
}

fn main() {
    greet("world");
}

# Choosing a license

A short decision tree, not a legal opinion. If your project handles money, regulated data, or trademarks, get a lawyer.

## Decision tree

1. **Do you want users to be able to use the code in proprietary products without restriction?**
   - Yes → MIT, Apache 2.0, or BSD-3.
   - No → MPL 2.0, GPL-3.0, AGPL-3.0.

2. **Do you want an explicit patent grant** (so contributors who hold patents can't sue users)?
   - Yes → **Apache 2.0** (or MPL 2.0 / GPL-3.0).
   - No / don't care → MIT / BSD-3.

3. **Do you want to require derivative works to be open source too** (copyleft)?
   - Strong copyleft, file-level → **MPL 2.0**.
   - Strong copyleft, project-wide → **GPL-3.0**.
   - Strong copyleft + network use as distribution → **AGPL-3.0** (the SaaS-loophole closer).

## Rough recommendations

| Project shape | Default | Why |
| --- | --- | --- |
| Library you want widely adopted | **Apache 2.0** | Permissive + patent grant; corporate-lawyer-friendly |
| Tiny library / utility | **MIT** | Simplest possible; broadly understood |
| Application you want to be open forever | **GPL-3.0** | Copyleft; derivatives stay open |
| Library you want copyleft-but-mixable | **MPL 2.0** | File-level copyleft; modifications stay open but can be combined with non-MPL code |
| SaaS-as-product you want to keep open against AWS | **AGPL-3.0** | Network use triggers copyleft; cloud forks must release source |
| Something so simple it shouldn't have a license | **0BSD** or **Unlicense** | Public-domain equivalent |

## What MIT vs Apache 2.0 actually differ on

- **Patent grant**: Apache has one explicit, MIT doesn't. If anyone working on the code holds patents that the code might infringe, Apache is safer.
- **Length**: MIT fits on a postcard, Apache is several pages.
- **NOTICE file**: Apache requires you to preserve a NOTICE file if upstream had one; MIT just requires the license header.
- **Adoption**: MIT is more common in the JS / Ruby / single-author worlds; Apache is more common at companies (CNCF projects default to Apache).

If you can't decide between them: **Apache 2.0**. It's strictly more protective for both users and contributors, and the extra length isn't a real cost.

## What to put in the repo

- `LICENSE` (no extension) at the repo root, full text. SPDX detector reads this; GitHub's "License" UI reads this.
- `SPDX-License-Identifier: <id>` header in source files (optional but nice — `// SPDX-License-Identifier: Apache-2.0`).
- License section in the README pointing at `LICENSE`.

## Re-licensing later

Hard. Re-licensing requires consent from every contributor (or a CLA / DCO that pre-grants it). The further along you are, the harder it gets. Pick once, change rarely.

If you're unsure between MIT and Apache 2.0 and care about getting it right: pick Apache 2.0.

## Resources

- [Choose a License](https://choosealicense.com/) — a longer-form decision guide from GitHub.
- [SPDX License List](https://spdx.org/licenses/) — canonical identifiers.
- [tldrlegal](https://tldrlegal.com/) — plain-English summaries, useful for sanity-checking what a license means.

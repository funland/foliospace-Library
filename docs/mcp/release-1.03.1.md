# FolioSpace MCP 1.03.1

Independent agent-side release. FolioSpace Library / Docker remains 1.03.

- Nine new read-only movie-library tools: works, work details, seasons, episodes, file versions, availability, per-file playback state, continue watching and next up.
- Capability discovery before querying; old servers can return explicitly labelled legacy file lists. Unsupported work filters never silently disappear.
- Explicit profile IDs are validated and preserved. Authentication failures do not fall back to a different route or profile. HTTP conflicts keep their structured code and state.
- Pagination defaults to 20, caps at 100; new tools bound responses to 2 MiB and do not follow redirects or expose upstream diagnostic bodies.
- All existing book/game/video/state/scan tool names and behavior remain available. This release adds no mutation tools.
- Four independent packages: macOS Intel/Apple Silicon and Linux AMD64/ARM64, with SHA-256 checksums.
- Installer requires valid checksums, tests initialization before atomic replacement, preserves URL/token configuration, and retains one previous binary for rollback.

Reconnect your MCP host after upgrading. Existing running processes do not automatically change when the executable is replaced. The package is a stdio bridge, not a new network service.

Canonical packages and installer: this GitHub release, tagged `mcp-v1.03.1`. The older website installer is not the release authority. MCP tags do not trigger Docker builds or replace the server's Latest release.

# TODO

## Testing

- [x] Add integration tests for the remote config server (client registration, token sync, webhook push/pull)
- [x] Add integration tests for BoltDB store (encryption round-trip, concurrent access, file watch reload)
- [x] Add end-to-end tests for the `run` command (proxy lifecycle, env var injection, cleanup on exit)
- [x] Add tests for remote webhook receiver (signature verification, event filtering, error handling)
- [x] Add tests for remote client periodic sync (interval timing, conflict resolution, network failure recovery)
- [x] Add store tests for edge cases (expired tokens, corrupt data, large policies, db file permissions)

- [ ] Fix memory formatting issues

## Package Manager Distribution

- [ ] Publish to Homebrew (macOS/Linux): create a tap at `rickcrawford/homebrew-tap` with a formula
- [ ] Publish to APT (Debian/Ubuntu): build `.deb` packages and host a PPA or use GitHub releases
- [ ] Publish to Chocolatey (Windows): create a `.nuspec` package and submit to the community repository
- [ ] Update `docs/DISTRIBUTION.md` with install instructions for each package manager
- [ ] Add GoReleaser config to automate package builds on release tags

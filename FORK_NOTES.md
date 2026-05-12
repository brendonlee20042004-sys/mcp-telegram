# Fork notes

This is a fork of [Prgebish/mcp-telegram](https://github.com/Prgebish/mcp-telegram).

The upstream project provides excellent architectural foundations:

- Default-deny ACL with per-chat permissions
- Token-bucket rate limiting on every MTProto RPC
- Typed peer references that prevent ID collisions
- Symlink-aware filesystem boundary for media
- Lazy peer resolution avoiding `FLOOD_WAIT`

This fork extends it toward production use. See [`progress.txt`](progress.txt)
for the full roadmap and current sprint.

## Why a fork?

After comparing ~20 Telegram MCP servers, this one had the strongest security
fundamentals. Rather than reimplement that foundation (cost: ~2 months) we
build on it.

## How to sync from upstream

```bash
git fetch upstream
git checkout main
git merge upstream/main
git push origin main
```

## Credit

All baseline work belongs to the upstream author. This fork only adds
incremental features and is also under the [MIT license](LICENSE).

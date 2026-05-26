# pkg/

本目录存放被 [`apps/`](../apps/) 中模块 import 的可复用 Go 库，不直接产出二进制。

| 目录 | 用途 | 集成方 |
|------|------|--------|
| [music/](music/README.md) | 本地曲库扫描、HTTP 文件服务、LX Sync Server 兜底 | [`apps/chat`](../apps/chat/README.md) |

## 新增公共库

如果你在多个 `apps/` 模块之间想复用 Go 代码，请把它放到 `pkg/<name>/`，并在使用方的 `go.mod` 中加入：

```go
require github.com/cxjava/open-xiaoai/pkg/<name> v0.0.0

replace github.com/cxjava/open-xiaoai/pkg/<name> => ../../pkg/<name>
```

不要把仅供单个 app 使用的内部代码放在这里——优先用 `apps/<name>/internal/` 或包内子目录。

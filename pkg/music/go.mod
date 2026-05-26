module github.com/cxjava/open-xiaoai/packages/music-go

go 1.26

require (
	github.com/dhowden/tag v0.0.0-20240417053706-3d75831295e8
	github.com/cxjava/open-xiaoai/packages/client-go v0.0.0
	golang.org/x/sync v0.20.0
)

require (
	github.com/coder/websocket v1.8.14 // indirect
	github.com/google/uuid v1.6.0 // indirect
)

replace github.com/cxjava/open-xiaoai/packages/client-go => ../client-go

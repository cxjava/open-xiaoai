module github.com/cxjava/open-xiaoai/examples/chat-go

go 1.26

require (
	github.com/coder/websocket v1.8.14
	github.com/cxjava/open-xiaoai/packages/client-go v0.0.0
	github.com/cxjava/open-xiaoai/packages/music-go v0.0.0
	github.com/sashabaranov/go-openai v1.41.2
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/dhowden/tag v0.0.0-20240417053706-3d75831295e8 // indirect
	github.com/google/uuid v1.6.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
)

replace (
	github.com/cxjava/open-xiaoai/packages/client-go => ../../packages/client-go
	github.com/cxjava/open-xiaoai/packages/music-go => ../../packages/music-go
)

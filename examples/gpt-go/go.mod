module github.com/idootop/open-xiaoai/examples/gpt-go

go 1.25.8

require (
	github.com/coder/websocket v1.8.14
	github.com/idootop/open-xiaoai/packages/client-go v0.0.0
	github.com/sashabaranov/go-openai v1.41.2
	gopkg.in/yaml.v3 v3.0.1
)

require github.com/google/uuid v1.6.0 // indirect

replace github.com/idootop/open-xiaoai/packages/client-go => ../../packages/client-go

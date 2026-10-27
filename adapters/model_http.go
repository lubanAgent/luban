package adapters

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"git.yukework.com/pkg/golib/v2/base"
	"github.com/lubanAgent/luban/conf"
	luban "github.com/lubanAgent/luban/sdk"
)

// modelHTTP 把 SDK 发模型请求时使用的 HTTP 能力替换为公司 golib。
//
// 调用过程是：SDK 组装模型请求 → 本文件经 golib 发送 → SDK 解析模型响应。
// OpenAI/Anthropic 协议、模型连接选择、Tool 和 Token 用量解析仍由 SDK 处理；
// 这里负责发送、连接复用、超时和按配置重试。
type modelHTTP struct {
	settings conf.ModelHTTP
	// transport 保存底层连接池，多个 Agent 的请求可以复用连接。
	transport http.RoundTripper
}

// 只要实现标准 Do 方法，框架就可以使用这个公司 HTTP 客户端。
var _ luban.HTTPDoer = (*modelHTTP)(nil)

// newModelHTTP 在启动时创建共享的 HTTP 传输对象，不会立即调用模型。
// 未填写时，总请求超时默认 120 秒，建立连接的超时默认 3 秒。
func newModelHTTP(settings conf.ModelHTTP) *modelHTTP {
	if settings.Timeout == 0 {
		settings.Timeout = 120 * time.Second
	}
	if settings.ConnectTimeout == 0 {
		settings.ConnectTimeout = 3 * time.Second
	}
	client := &base.ApiClient{ConnectTimeout: settings.ConnectTimeout}
	return &modelHTTP{settings: settings, transport: client.GetTransPort()}
}

// Do 在真正调用模型时执行。request 已经包含 SDK 选定的地址、认证头和请求体。
func (client *modelHTTP) Do(request *http.Request) (*http.Response, error) {
	// 从 SDK 的标准 Context 中取回本次 Gin Context，交给 golib 关联请求日志。
	ctx, ok := luban.GinContext(request.Context())
	if !ok {
		return nil, fmt.Errorf("company model HTTP requires a Luban Gin context")
	}
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	// 同时遵守本次 Run 的取消信号和模型 HTTP 总超时。
	parent, cancel := context.WithTimeout(request.Context(), client.settings.Timeout)
	// 收到响应头时，模型响应体可能还没有读完，不能立即 cancel。
	// 请求成功后把清理责任交给响应体，等 SDK 读完并 Close 时再释放。
	keepContext := false
	defer func() {
		if !keepContext {
			cancel()
		}
	}()
	var body []byte
	if request.Body != nil {
		// golib 发送原始字节；保留 SDK 已编码好的 JSON，不在这里重新组装协议。
		defer request.Body.Close()
		var err error
		body, err = io.ReadAll(request.Body)
		if err != nil {
			return nil, fmt.Errorf("read model HTTP request")
		}
	}
	// 每次调用创建独立 ApiClient，地址、请求头等只属于本次请求。
	// 底层连接池仍共用，避免多个 Agent 并发时串用别人的地址或 Key。
	gateway := &base.ApiClient{
		Service: "llm", Domain: request.URL.Scheme + "://" + request.URL.Host,
		Timeout: client.settings.Timeout, Retry: client.settings.Retry,
		// 关闭 golib 对模型请求/响应正文的日志打印，由 SDK 的 Trace 记录执行数据。
		MaxReqBodyLen: -1, MaxRespBodyLen: -1,
		HttpClient: &http.Client{
			Transport:     boundTransport{parent: parent, base: client.transport},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	headers := make(map[string]string, len(request.Header))
	for key := range request.Header {
		headers[key] = request.Header.Get(key)
	}
	// HttpStream 将响应体交回 SDK，由 SDK 限量读取、解析并关闭。
	// 公司 golib 负责补充请求日志标识，并按 Retry 配置处理传输重试。
	response, err := gateway.HttpStream(ctx, request.Method, request.URL.RequestURI(), base.HttpRequestOptions{
		Encode: base.EncodeRawByte, RequestBody: body, Headers: headers,
	})
	if err != nil {
		// golib 的重试错误未必保留原始取消原因。
		// 如果实际是超时或取消，优先返回 Context 中的原因，方便上层判断。
		if cause := parent.Err(); cause != nil {
			err = cause
		}
		return response, err
	}
	if response != nil && response.Body != nil {
		// 响应体还要继续读取，总超时在读取期间也必须有效。
		response.Body = &contextBody{ReadCloser: response.Body, cleanup: cancel}
		keepContext = true
	}
	return response, nil
}

// boundTransport 为 golib 最终发出的请求补回本次 Run 的 Context。
type boundTransport struct {
	parent context.Context
	base   http.RoundTripper
}

func (transport boundTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// golib 会创建自己的 HTTP 请求，所以在真正发送前重新绑定 Context。
	// 每次重试也经过这里，确保上层已经取消时不会继续独立跑请求。
	return transport.base.RoundTrip(request.Clone(transport.parent))
}

// contextBody 包装响应体，把“关闭响应体”和“释放超时 Context”放在一起。
type contextBody struct {
	io.ReadCloser
	cleanup func()
}

func (body *contextBody) Close() error {
	defer body.cleanup()
	return body.ReadCloser.Close()
}

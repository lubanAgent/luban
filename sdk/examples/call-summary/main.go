// 通话摘要示例：从 JSON 配置到 Gin 路由，走完整的 Luban 执行流程。
// 默认用内存 HTTP 请求演示，不监听端口、不访问模型或数据库。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
	lubanhttp "github.com/lubanAgent/luban/sdk/entrypoint/http"
)

const sampleInput = `{"text":"客户：想了解周末的课程。顾问：明天下午发课程安排，再电话确认。"}`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "examples/call-summary/agent.json", "Agent JSON 配置路径，相对于当前工作目录")
	listen := flag.String("listen", "", "可选：监听地址，例如 127.0.0.1:8080；默认只执行一次内存请求")
	flag.Parse()

	// 启动时做一次：读配置 → New → Register。
	rt, err := newRuntime(*configPath)
	if err != nil {
		return err
	}

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	// 接到现有业务时，把这一行放进自己的路由注册处即可。
	// Handler 内部调用 Run；首节点 PrepareCall 从 ctx 解析 HTTP 请求。
	router.POST("/summary", lubanhttp.Handler(rt, "callSummary", render))

	if *listen != "" {
		fmt.Printf("模拟模型服务：http://%s/summary（返回固定演示摘要）\n", *listen)
		return http.ListenAndServe(*listen, router)
	}

	// 默认模式也经过上面同一个路由和 Handler，只是不占用本机端口。
	req := httptest.NewRequest(http.MethodPost, "/summary", strings.NewReader(sampleInput))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		return fmt.Errorf("示例请求失败：HTTP %d %s", response.Code, response.Body.String())
	}
	fmt.Println("模式：模拟模型，不访问外部服务")
	fmt.Println("输入：" + sampleInput)
	fmt.Println("输出：" + response.Body.String())
	return nil
}

func newRuntime(configPath string) (*luban.Runtime, error) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置：%w", err)
	}
	var config luban.Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("解析配置：%w", err)
	}

	rt, err := luban.New(luban.Options{
		Config: config,
		Models: map[string]luban.ModelInvoker{"demo": demoModel{}},
	})
	if err != nil {
		return nil, err
	}
	if err := rt.Register(map[string]any{
		"PrepareCall": PrepareCall,
		"BuildResult": BuildResult,
	}, nil, nil); err != nil {
		return nil, err
	}
	return rt, nil
}

// PrepareCall 是配置中的首个业务节点：解析请求，准备动态模型消息。
func PrepareCall(ctx *gin.Context) (luban.Request, error) {
	var input struct {
		Text string `json:"text" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&input); err != nil {
		return luban.Request{}, err
	}
	return luban.Request{Messages: []luban.Message{{
		Role:    luban.RoleUser,
		Content: []luban.Part{{Type: luban.PartText, Text: input.Text}},
	}}}, nil
}

// CallSummary 是业务对外返回的数据。此例没有声明模型结构化输出；
// Agent 交付 string，由后一个业务函数组装这个结构体。
type CallSummary struct {
	Summary string `json:"summary"`
}

// BuildResult 接收上一 Agent 的文本，整理接口结果；本例不读写数据库。
func BuildResult(_ *gin.Context, summary string) CallSummary {
	return CallSummary{Summary: summary}
}

// render 沿用业务自己的响应格式。此例统一把执行失败返回为 HTTP 500。
func render(ctx *gin.Context, output any, err error) {
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, output)
}

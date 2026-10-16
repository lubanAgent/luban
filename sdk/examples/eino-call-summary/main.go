// 使用原生 Eino 完成与 call-summary 相同的通话摘要。
// 两边都用固定模拟回复，经过 Gin 路由，默认不监听端口或访问外部服务。
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
)

const sampleInput = `{"text":"客户：想了解周末的课程。顾问：明天下午发课程安排，再电话确认。"}`

type CallSummary struct {
	Summary string `json:"summary"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	listen := flag.String("listen", "", "可选：监听地址；默认只执行一次内存请求")
	flag.Parse()

	// Eino 可以直接使用 Chain/Lambda 接普通函数，不需要手写 ADK Agent。
	// 流程固定时，允许启动编译后复用，不刻意要求每次重建。
	chain, err := newChain()
	if err != nil {
		return err
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/summary", func(ctx *gin.Context) {
		result, err := chain.Invoke(ctx.Request.Context(), ctx)
		render(ctx, result, err)
	})
	if *listen != "" {
		fmt.Printf("模拟模型服务：http://%s/summary（返回固定演示摘要）\n", *listen)
		return http.ListenAndServe(*listen, router)
	}

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

func newChain() (compose.Runnable[*gin.Context, CallSummary], error) {
	return compose.NewChain[*gin.Context, CallSummary]().
		AppendLambda(compose.InvokableLambda(PrepareCall)).
		AppendChatModel(demoModel{}).
		AppendLambda(compose.InvokableLambda(BuildResult)).
		Compile(context.Background())
}

func PrepareCall(_ context.Context, ctx *gin.Context) ([]*schema.Message, error) {
	var input struct {
		Text string `json:"text" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&input); err != nil {
		return nil, err
	}
	return []*schema.Message{
		schema.SystemMessage("用一句话总结通话中的诉求和后续安排。"),
		schema.UserMessage(input.Text),
	}, nil
}

func BuildResult(_ context.Context, message *schema.Message) (CallSummary, error) {
	return CallSummary{Summary: message.Content}, nil
}

func render(ctx *gin.Context, output any, err error) {
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, output)
}

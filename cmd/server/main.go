package main

import (
	"context"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"xstudio"
	"xstudio/internal/core"
	"xstudio/internal/modules/accounts"
	"xstudio/internal/modules/assembly"
	"xstudio/internal/modules/assets"
	"xstudio/internal/modules/assistant"
	"xstudio/internal/modules/copywriting"
	"xstudio/internal/modules/creative"
	"xstudio/internal/modules/finance"
	"xstudio/internal/modules/inventory"
	"xstudio/internal/modules/kb"
	"xstudio/internal/modules/market"
	"xstudio/internal/modules/orders"
	"xstudio/internal/modules/pipeline"
	"xstudio/internal/modules/products"
	"xstudio/internal/modules/purchase"
	"xstudio/internal/modules/studio"
	"xstudio/internal/modules/tasks"
)

func main() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.LstdFlags)
	c, e := core.EnvConfig()
	if e != nil {
		log.Fatal("配置错误：", e)
	}
	web, e := fs.Sub(xstudio.Assets, "web/dist")
	if e != nil {
		log.Fatal("加载前端失败：", e)
	}
	slots, e := xstudio.Assets.ReadFile("槽位.json")
	if e != nil {
		log.Fatal("加载槽位失败：", e)
	}
	a, e := core.New(c, web, slots)
	if e != nil {
		log.Fatal("启动失败：", e)
	}
	modules := []core.Module{accounts.New(a), products.New(a), assets.New(a), pipeline.New(a), copywriting.New(a), studio.New(a), assistant.New(a), tasks.New(a), inventory.New(a), kb.New(a), purchase.New(a), orders.New(a), assembly.New(a), finance.New(a), creative.New(a), market.New(a)}
	if e = a.Start(modules...); e != nil {
		_ = a.Close()
		log.Fatal("启动失败：", e)
	}
	listener, e := net.Listen("tcp", c.Listen)
	if e != nil {
		_ = a.Close()
		log.Fatal("监听失败：", e)
	}
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	} else if host == "::" {
		host = "::1"
	}
	a.SetBaseURL("http://" + net.JoinHostPort(host, port))
	server := &http.Server{Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("X Studio 已启动，监听 %s", listener.Addr())
	e = server.Serve(listener)
	if e != nil && e != http.ErrServerClosed {
		log.Print("HTTP 服务停止：", e)
	}
	if e = a.Close(); e != nil {
		log.Print("关闭数据库失败：", e)
	}
}

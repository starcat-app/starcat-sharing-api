// Package main 是 starcat-sharing-api 程序的入口点
package main

import (
	"html/template"
	"log"
	"net/http"
	"os"

	"github.com/dong4j/starcat-sharing-api/internal/handler"
	"github.com/dong4j/starcat-sharing-api/internal/store"
)

func main() {
	// 初始化配置
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "https://starcat.ink"
	}

	// 数据文件路径: 优先读取 STORE_FILE, 缺省使用当前目录的 data.json (本地开发默认)
	storeFile := os.Getenv("STORE_FILE")
	if storeFile == "" {
		storeFile = "data.json"
	}

	// 初始化存储
	s, err := store.NewMemoryStore(storeFile)
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}

	// 加载模板
	var templates *template.Template
	if tmpl, err := template.ParseGlob("templates/*.html"); err != nil {
		log.Fatalf("Failed to parse templates: %v", err)
	} else {
		templates = tmpl
	}

	// 初始化处理器
	shareHandler := handler.NewShareHandler(s, templates, baseURL)

	// 注册路由
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/share", shareHandler.HandleCreateShare)
	mux.HandleFunc("GET /s/{id}", shareHandler.HandleViewShare)
	// 健康检查: Fly.io http_service.checks 用, 固定返回 200
	mux.HandleFunc("GET /healthz", healthzHandler)

	// 启动服务
	port := os.Getenv("PORT")
	if port == "" {
		port = "5001"
	}

	log.Printf("Starting server on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

// healthzHandler 健康检查（Fly.io http_service.checks 使用）
func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

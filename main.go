package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"alemonjs-setup/internal/web"
)

//go:embed all:dist
var staticFiles embed.FS

// 前端页面

//go:embed all:templates
var templateFiles embed.FS

// 开发模板文件 + 机器人启动目录

var Version = "dev"

func main() {
	port := flag.String("port", env("PORT", "17390"), "Web 服务端口")
	showVersion := flag.Bool("version", false, "显示版本号")
	flag.Parse()

	if *showVersion {
		fmt.Println(Version)
		return
	}

	server := &http.Server{
		Addr:              "127.0.0.1:" + *port,
		Handler:           web.NewServer(Version, staticFiles, templateFiles),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("alemonjs-setup %s 已启动：http://localhost:%s", Version, *port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

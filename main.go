package main

import (
	//"fmt"
	"crypto/tls"
	"github.com/gorilla/mux"
	"log"
	"net"
	"net/http"
//	"connection_server_linux/inittool"
	"connection_server_linux/tcpnetwork"
	"connection_server_linux/router"
	"connection_server_linux/databasetool"
	"gorm.io/gorm"
)

// 全局数据库连接对象
var DB *gorm.DB

func main() {
	// 初始化数据库连接
	DB = databasetool.InitDB()
	
	// 启动TCP服务器
	//localIP := inittool.GetLocalIP()
	var err error

	tcpnetwork.TcpAddr, err = net.ResolveTCPAddr("tcp4", "0.0.0.0:12345")
	if err != nil {
		log.Fatal(err)
	}

	tcpListener, err := net.ListenTCP("tcp", tcpnetwork.TcpAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer tcpListener.Close()

	log.Printf("TCP服务器已启动，监听地址：%s", tcpnetwork.TcpAddr)

	// 启动HTTPS服务器
	route := mux.NewRouter()
	// 设置路由
	router.SetupRoutes(route)

	// 配置TLS
	tlsConfig := &tls.Config{
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		},
	}

	// 创建HTTPS服务器
	srv := &http.Server{
		Addr:         ":8443",
		Handler:      route,
		TLSConfig:    tlsConfig,
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler), 0),
	}

	go func() {
		log.Printf("HTTPS服务器已启动，监听地址：:8443")
		if err := srv.ListenAndServeTLS("certs/server.crt", "certs/server.key"); err != nil {
			log.Fatal(err)
		}
	}()

	tcpnetwork.InitDBConnection(DB)

	// 处理TCP连接
	for {
		conn, err := tcpListener.Accept()
		if err != nil {
			log.Printf("接受连接失败: %v", err)
			continue
		}
		go tcpnetwork.HandleConnection(conn)
		
	}
}
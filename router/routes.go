package router

import (
	"github.com/gorilla/mux"
	"net/http"
	"connection_server_linux/logincheck"
	"connection_server_linux/tcpnetwork"
)

// setupRoutes 配置所有HTTP路由
func SetupRoutes(router *mux.Router) {

	router.Use(logincheck.AuthMiddleware)
	// 登录路由
	router.HandleFunc("/api/login", tcpnetwork.LoginHandler).Methods("POST")
	
	// API路由
	apiRouter := router.PathPrefix("/api").Subrouter()
	
	apiRouter.HandleFunc("/server-info", tcpnetwork.GetServerInfoHandler).Methods("GET")
	apiRouter.HandleFunc("/clients", tcpnetwork.GetClientsHandler).Methods("GET")
	apiRouter.HandleFunc("/clients/{id}/kick", tcpnetwork.KickClientHandler).Methods("POST")
	apiRouter.HandleFunc("/clients/{id}/message", tcpnetwork.SendMessageHandler).Methods("POST")
	
	// 静态文件服务
	fileServer := http.FileServer(http.Dir("static"))

	// 根路径路由处理
	router.PathPrefix("/").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/login.html", http.StatusSeeOther)
			return
		}
		fileServer.ServeHTTP(w, r)
	}))
}
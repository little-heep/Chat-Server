package tcpnetwork
//http请求处理
import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"net/http"
	"time"
	"crypto/rand"
	"encoding/base64"
	"log"
	"connection_server_linux/user"
	"connection_server_linux/logincheck"
	
)

// 生成随机session ID
func GenerateSessionID() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

// 登录处理函数
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&credentials); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "无效的请求格式"})
		return
	}

	// 验证用户名和密码
	if credentials.Username == "notlike" && credentials.Password == "serve678" {
		// 生成session ID并创建会话
		sessionID := GenerateSessionID()
		logincheck.GlobalSessionManager.CreateSession(credentials.Username,sessionID)

		// 设置session cookie
		sessionCookie := http.Cookie{
			Name: "sessionID",
			Value: sessionID,
			Path: "/",
			HttpOnly: true,
			Secure: true,
			Expires: time.Now().Add(24 * time.Hour),
		}
		http.SetCookie(w, &sessionCookie)
		log.Println("设置新cookie成功，cookie的value为：", sessionCookie.Value)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	} else {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "用户名或密码错误"})
	}
}

// 获取所有客户端列表
func GetClientsHandler(w http.ResponseWriter, r *http.Request) {
	user.Manager.Mutex.RLock()
	clients := make([]user.Client, 0, len(user.Manager.Clients))
	for _, client := range user.Manager.Clients {
		clients = append(clients, *client)
	}
	user.Manager.Mutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(clients)
}

// 踢出指定客户端
func KickClientHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	clientID := vars["id"]

	user.Manager.Mutex.Lock()
	if client, exists := user.Manager.Clients[clientID]; exists {
		client.Conn.Close()
		delete(user.Manager.Clients, clientID)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "客户端 %s 已被踢出", clientID)
	} else {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "未找到客户端 %s", clientID)
	}
	user.Manager.Mutex.Unlock()
}

// 发送消息给指定客户端
func SendMessageHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	clientID := vars["id"]

	var message struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	user.Manager.Mutex.RLock()
	if client, exists := user.Manager.Clients[clientID]; exists {
		_, err := client.Conn.Write([]byte(message.Content))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "发送消息失败: %v", err)
		} else {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "消息已发送到客户端 %s", clientID)
		}
	} else {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "未找到客户端 %s", clientID)
	}
	user.Manager.Mutex.RUnlock()
}

// 获取服务器IP和端口信息
func GetServerInfoHandler(w http.ResponseWriter, r *http.Request) {
	serverInfo := struct {
		IP   string `json:"ip"`
		Port int    `json:"port"`
	}{
		IP:   TcpAddr.IP.String(),
		Port: TcpAddr.Port,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(serverInfo)
}
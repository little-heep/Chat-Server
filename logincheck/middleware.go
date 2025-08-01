package logincheck
//中间件
import (
	"net/http"
	"time"
	"strings"
	"log"
)

// AuthMiddleware 中间件：检查用户是否已登录
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 排除登录页面和登录API
		if r.URL.Path == "/login.html" || strings.HasPrefix(r.URL.Path, "/api/login") {
			next.ServeHTTP(w, r)
			log.Println("登录界面无需验证")
			return
		}

		// 获取并验证sessionID
		sessionCookie, err := r.Cookie("sessionID")
		//log.Println("验证的cookie的value为：",sessionCookie.Value)
		if err != nil || sessionCookie.Value == "" || !GlobalSessionManager.ValidateSession(sessionCookie.Value) {
			
			// 清除无效的cookie
			http.SetCookie(w, &http.Cookie{
				Name:     "sessionID",
				Value:    "",
				Path:     "/",
				HttpOnly: true,
				Expires:  time.Unix(0, 0),
			})

			// 区分API请求和页面请求的响应
			if strings.HasPrefix(r.URL.Path, "/api/") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"请先登录"}`))  
				return
			}

			// 重定向到登录页面
			http.Redirect(w, r, "/login.html", http.StatusSeeOther)
			return
		}

		// 验证通过，继续处理请求
		next.ServeHTTP(w, r)
		log.Println("验证通过")
	})
}
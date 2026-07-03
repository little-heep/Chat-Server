package tcpnetwork

import (
	"connection_server_linux/databasetool"
	"connection_server_linux/friendupdate"
	"connection_server_linux/user"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// FileHeader 文件头信息
type FileHeader struct {
	Type      string   `json:"type"`      // 固定为"file_transfer"
	Filename  string   `json:"filename"`  // 文件名
	Size      FileSize `json:"size"`      // 文件大小(兼容字符串/数字)
	SendID    string   `json:"sendid"`    // 发送者ID
	ReceiveID string   `json:"receiveid"` // 接收者ID
}

// FileSize 兼容字符串和数字的文件大小字段
type FileSize string

// UnmarshalJSON 兼容客户端发送的数字或字符串大小
func (s *FileSize) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		*s = ""
		return nil
	}

	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*s = FileSize(value)
		return nil
	}

	var value json.Number
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*s = FileSize(value.String())
	return nil
}

// String 返回字符串形式的文件大小
func (s FileSize) String() string {
	return string(s)
}

// FileStorage 文件存储结构
type FileStorage struct {
	Filename   string
	FilePath   string // 添加文件路径字段
	FileSize   int64
	Received   int64
	SendTime   time.Time
	SenderID   string
	ReceiverID string
}

var (
	pendingFiles    = make(map[string]*FileStorage)   // 待接收文件缓存
	uploadSessions  = make(map[string]*UploadSession) // 正在接收中的文件会话
	fileMutex       sync.Mutex                        // 文件操作互斥锁
	fileStoragePath string                            // 文件存储目录
)

var (
	TcpAddr *net.TCPAddr // 全局TCP地址
	db      *gorm.DB     // 添加全局db引用
)

func InitDBConnection(globalDB *gorm.DB) {
	db = globalDB
}

func init() {
	if wd, err := os.Getwd(); err == nil {
		fileStoragePath = filepath.Join(wd, "file_storage")
	} else {
		fileStoragePath = filepath.Join(".", "file_storage")
	}

	if err := os.MkdirAll(fileStoragePath, 0755); err != nil {
		log.Printf("创建文件存储目录失败 %s: %v", fileStoragePath, err)
	}
}

// LoginRequest 客户端登录请求结构
type LoginRequest struct {
	Type     string `json:"type"` // 消息类型，固定为"login"
	Username string `json:"name"` // 用户ID
	Password string `json:"pwd"`  // 密码
}

// LoginResponse 登录响应结构
type LoginResponse struct {
	Type    string `json:"type"`    // 消息类型，固定为"login_response"
	Success bool   `json:"success"` // 是否成功
	Message string `json:"message"` // 返回消息
}

// RegisterRequest 客户端注册请求结构
type RegisterRequest struct {
	Type     string `json:"type"` // 消息类型，固定为"register"
	Username string `json:"name"` // 用户名
	Password string `json:"pwd"`  // 用户密码
}

// RegisterResponse 注册响应结构
type RegisterResponse struct {
	Type    string `json:"type"`    // 消息类型，固定为"register_response"
	Status  string `json:"status"`  // 状态
	UserID  uint   `json:"userid"`  // 用户ID
	Message string `json:"message"` // 返回消息
}

// UploadSession 文件上传中的临时会话
type UploadSession struct {
	FileKey    string
	FilePath   string
	File       *os.File
	FileSize   int64
	Received   int64
	SenderID   string
	ReceiverID string
	Filename   string
}

// readFramedPacket 读取 8 字节包头：4字节类型 + 4字节长度
func readFramedPacket(conn net.Conn) (uint32, []byte, error) {
	header := make([]byte, 8)
	if _, err := io.ReadFull(conn, header); err != nil {
		return 0, nil, fmt.Errorf("读取包头失败: %v", err)
	}

	packetType := binary.BigEndian.Uint32(header[:4])
	payloadLen := binary.BigEndian.Uint32(header[4:])
	if payloadLen == 0 {
		return packetType, nil, errors.New("空消息")
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return packetType, nil, fmt.Errorf("读取消息体失败: %v", err)
	}

	return packetType, payload, nil
}

// writeFramedPacket 写入 8 字节包头和消息体
func writeFramedPacket(conn net.Conn, packetType uint32, payload []byte) error {
	header := make([]byte, 8)
	binary.BigEndian.PutUint32(header[:4], packetType)
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))

	if _, err := conn.Write(header); err != nil {
		return fmt.Errorf("写入包头失败: %v", err)
	}
	if len(payload) == 0 {
		return nil
	}
	if _, err := conn.Write(payload); err != nil {
		return fmt.Errorf("写入消息体失败: %v", err)
	}
	return nil
}

// writeFramedBytes 写入 JSON 包，类型固定为 1
func writeFramedBytes(conn net.Conn, payload []byte) error {
	return writeFramedPacket(conn, 1, payload)
}

// sendLoginResponse 发送登录响应
func sendLoginResponse(conn net.Conn, success bool, message string) {
	response := LoginResponse{
		Type:    "login_response",
		Success: success,
		Message: message,
	}
	responseBytes, _ := json.Marshal(response)
	_ = writeFramedBytes(conn, responseBytes)
}

// sendRegisterResponse 发送注册响应
func sendRegisterResponse(conn net.Conn, status string, userID uint, message string) {
	response := RegisterResponse{
		Type:    "register_response",
		Status:  status,
		UserID:  userID,
		Message: message,
	}
	responseBytes, _ := json.Marshal(response)
	_ = writeFramedBytes(conn, responseBytes)
}

// handleLogin 处理登录验证
func handleLogin(conn net.Conn, cleanData []byte) (*user.Client, error) {
	fmt.Println("清理后的登录数据:", string(cleanData))

	var loginReq LoginRequest
	if err := json.Unmarshal(cleanData, &loginReq); err != nil {
		return nil, fmt.Errorf("解析登录数据失败: %v", err)
	}

	if loginReq.Type != "login" {
		return nil, errors.New("第一条消息必须是登录请求")
	}

	username := loginReq.Username
	userRecord, err := databasetool.FindUserByName(db, username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			sendLoginResponse(conn, false, "账号不存在")
			return nil, errors.New("账号不存在")
		}
		sendLoginResponse(conn, false, "数据库错误")
		return nil, fmt.Errorf("数据库查询错误: %v", err)
	}

	if userRecord.Password != loginReq.Password {
		sendLoginResponse(conn, false, "用户名或密码错误")
		return nil, errors.New("用户名或密码错误")
	}

	userRecord.Status = 1
	userRecord.Ip = conn.RemoteAddr().(*net.TCPAddr).IP.String()
	userRecord.LeaveTime = time.Now()
	if err := db.Save(&userRecord).Error; err != nil {
		sendLoginResponse(conn, false, "服务器错误")
		return nil, fmt.Errorf("更新用户状态失败: %v", err)
	}

	client := &user.Client{
		Conn:        conn,
		ID:          fmt.Sprintf("%d", userRecord.ID),
		IP:          conn.RemoteAddr().(*net.TCPAddr).IP.String(),
		ConnectTime: time.Now(),
		LastActive:  time.Now(),
		Friends:     make([]user.FriendInfo, 0),
	}

	user.Manager.Mutex.Lock()
	user.Manager.Clients[client.ID] = client
	user.Manager.Mutex.Unlock()

	sendLoginResponse(conn, true, "id:"+fmt.Sprintf("%d", userRecord.ID))
	return client, nil
}

// handleRegister 处理注册请求
func handleRegister(conn net.Conn, cleanData []byte) error {
	var registerReq RegisterRequest
	if err := json.Unmarshal(cleanData, &registerReq); err != nil {
		return fmt.Errorf("解析注册数据失败: %v", err)
	}

	if registerReq.Type != "register" {
		return errors.New("首条消息必须是注册请求")
	}

	if registerReq.Username == "" || registerReq.Password == "" {
		sendRegisterResponse(conn, "fail", 0, "用户名或密码不能为空")
		return errors.New("用户名或密码不能为空")
	}

	if _, err := databasetool.FindUserByName(db, registerReq.Username); err == nil {
		sendRegisterResponse(conn, "fail", 0, "用户名已存在")
		return errors.New("用户名已存在")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		sendRegisterResponse(conn, "fail", 0, "数据库错误")
		return fmt.Errorf("查询用户名失败: %v", err)
	}

	ip := ""
	if tcpAddr, ok := conn.RemoteAddr().(*net.TCPAddr); ok && tcpAddr.IP != nil {
		ip = tcpAddr.IP.String()
	}

	userID, err := databasetool.RegisterUser(db, registerReq.Username, registerReq.Password, ip)
	if err != nil {
		sendRegisterResponse(conn, "fail", 0, "注册失败")
		return fmt.Errorf("注册用户失败: %v", err)
	}

	sendRegisterResponse(conn, "success", userID, "注册成功")
	return nil
}

// handleInitialConnection 处理TCP首条消息，支持登录和注册
func handleInitialConnection(conn net.Conn) (*user.Client, error) {
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	packetType, cleanData, err := readFramedPacket(conn)
	if err != nil {
		return nil, err
	}
	if packetType != 1 {
		return nil, fmt.Errorf("首包类型必须是JSON，收到类型: %d", packetType)
	}

	var msgType struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(cleanData, &msgType); err != nil {
		return nil, fmt.Errorf("解析首条消息类型失败: %v", err)
	}

	switch msgType.Type {
	case "login":
		return handleLogin(conn, cleanData)
	case "register":
		if err := handleRegister(conn, cleanData); err != nil {
			return nil, err
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("未知的首条消息类型: %s", msgType.Type)
	}
}

// HandleConnection 处理新TCP连接
func HandleConnection(conn net.Conn) {
	defer conn.Close()

	client, err := handleInitialConnection(conn)
	if err != nil {
		log.Printf("首包处理失败: %v", err)
		return
	}
	if client == nil {
		return
	}
	log.Printf("新客户端连接: %s", client.ID)

	// 2. 初始化好友列表
	if err := setupFriendList(client); err != nil {
		log.Printf("初始化好友列表失败 %s: %v", client.ID, err)
		return
	}

	// 3. 检查并发送待接收消息
	chats, err := databasetool.GetUnsendChatsByReciveID(db, client.ID)
	if err != nil {
		log.Printf("检查待接收消息失败 %s: %v", client.ID, err)
		return
	}

	for _, chat := range chats {
		// 处理不同类型的暂存消息
		if strings.HasPrefix(chat.Content, "file:") {
			// 处理文件消息
			filekey := strings.TrimPrefix(chat.Content, "file:")
			if err := checkPendingFiles(client, filekey); err != nil {
				log.Printf("发送待接收文件失败 %s: %v", client.ID, err)
			}
		} else if strings.HasPrefix(chat.Content, "addfriend_request:") {
			// 处理好友请求
			parts := strings.Split(chat.Content, ":")
			if len(parts) >= 3 {
				senderID := parts[1]
				senderName := parts[2]

				// 创建好友请求消息
				addFriendReq := map[string]interface{}{
					"type":    "addfriend_request",
					"addid":   senderID,
					"addname": senderName,
					"status":  1,
					"online":  true,
				}

				// 发送好友请求
				reqBytes, err := json.Marshal(addFriendReq)
				if err != nil {
					log.Printf("序列化好友请求失败 %s: %v", client.ID, err)
					continue
				}

				if err := writeFramedBytes(client.Conn, reqBytes); err != nil {
					log.Printf("发送好友请求失败 %s: %v", client.ID, err)
					continue
				}
			}
		} else if strings.HasPrefix(chat.Content, "friend_accepted:") {
			// 处理好友接受通知
			parts := strings.Split(chat.Content, ":")
			if len(parts) >= 3 {
				senderID := parts[1]
				senderName := parts[2]

				// 创建好友接受通知
				acceptNotice := map[string]interface{}{
					"type":     "friend_accepted",
					"userid":   senderID,
					"username": senderName,
					"status":   1,
					"online":   true,
				}

				// 发送好友接受通知
				noticeBytes, err := json.Marshal(acceptNotice)
				if err != nil {
					log.Printf("序列化好友接受通知失败 %s: %v", client.ID, err)
					continue
				}

				if err := writeFramedBytes(client.Conn, noticeBytes); err != nil {
					log.Printf("发送好友接受通知失败 %s: %v", client.ID, err)
					continue
				}
			}
		} else {
			// 处理普通消息
			chatMsg := user.ChatMessage{
				Type:      "message",
				SendID:    chat.Sendid,
				ReceiveID: chat.Reciveid,
				Content:   chat.Content,
				SendTime:  chat.SendTime.String(),
			}

			msgBytes, err := json.Marshal(chatMsg)
			if err != nil {
				log.Printf("序列化消息失败 %s: %v", client.ID, err)
				continue
			}

			if err := writeFramedBytes(client.Conn, msgBytes); err != nil {
				log.Printf("发送消息失败 %s: %v", client.ID, err)
				continue
			}
		}

		// 从数据库中删除已发送的消息
		if err := databasetool.DeleteUnsendChat(db, chat.Logid); err != nil {
			log.Printf("删除已发送消息失败 %s: %v", client.ID, err)
			continue
		}
	}

	// 4. 进入消息处理循环
	messageLoop(client)
}

// setupFriendList 初始化好友列表
func setupFriendList(client *user.Client) error {

	s := client.ID
	num, err := strconv.Atoi(s)
	if err != nil {
		log.Fatal("转换失败:", err)
	}
	// 查询用户数据
	userRecord, err := databasetool.FindUserById(db, num)
	if err != nil {
		return fmt.Errorf("查询用户失败: %v", err)
	}

	fmt.Println(userRecord.Relation)
	// 解析好友关系字节
	friendStatuses := friendupdate.AnalyzeRelationByte(userRecord.Relation)
	friendStatuses[0] = int(userRecord.ID) //第一个位置放自己的ID

	fmt.Println(friendStatuses)
	// 构建好友列表
	for i := 1; i < len(friendStatuses); i++ {
		if friendStatuses[i] == friendupdate.Friend {

			friendID := fmt.Sprintf("%d", i)
			friend, err := databasetool.FindUserById(db, i)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue // 跳过不存在的好友
				}
				fmt.Errorf("查询好友失败: %v", err)
				continue
			}

			client.Friends = append(client.Friends, user.FriendInfo{
				UserID: friendID,
				Name:   friend.Name,
				Status: friendStatuses[i],
				Online: friend.Status == 1,
			})
		}
	}

	// 发送好友列表
	friendListMsg := user.FriendListMessage{
		Type:    "friend_list",
		Friends: client.Friends,
	}
	msgBytes, err := json.Marshal(friendListMsg)
	if err != nil {
		return fmt.Errorf("序列化好友列表失败: %v", err)
	}

	if err := writeFramedBytes(client.Conn, msgBytes); err != nil {
		return fmt.Errorf("发送好友列表失败: %v", err)
	}

	return nil
}

// messageLoop 消息处理循环
func messageLoop(client *user.Client) {
	for {
		packetType, messageData, err := readFramedPacket(client.Conn)
		if err != nil {
			log.Printf("客户端 %s 断开连接: %v", client.ID, err)
			cleanupClient(client)
			return
		}

		client.LastActive = time.Now()
		switch packetType {
		case 1:
			if err := handleMessage(client, messageData); err != nil {
				log.Printf("处理JSON消息失败 %s: %v", client.ID, err)
			}
		case 2:
			if err := handleFileChunk(client, messageData); err != nil {
				log.Printf("处理文件数据失败 %s: %v", client.ID, err)
			}
		case 3:
			if err := handleFileHeaderPacket(client, messageData); err != nil {
				log.Printf("处理文件头失败 %s: %v", client.ID, err)
			}
		default:
			log.Printf("未知包类型 %d 来自 %s", packetType, client.ID)
		}
	}
}

// cleanupClient 清理客户端资源
func cleanupClient(client *user.Client) {

	if err := db.Model(&databasetool.User{}).Where("id = ?", client.ID).Updates(map[string]interface{}{
		"Status":    0, // 0表示离线
		"LeaveTime": time.Now(),
	}).Error; err != nil {
		log.Printf("更新用户状态失败 %s: %v", client.ID, err)
	}

	// 从管理器移除
	user.Manager.Mutex.Lock()
	delete(user.Manager.Clients, client.ID)
	user.Manager.Mutex.Unlock()

	// 更新数据库状态
	db := databasetool.InitDB()
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()

}

// handleMessage 处理单条消息
func handleMessage(client *user.Client, messageData []byte) error {
	log.Printf("收到来自 %s 的消息: %s", client.ID, messageData)

	messageStr := strings.TrimSpace(string(messageData))
	if len(messageStr) == 0 {
		return errors.New("空消息")
	}

	// 解析消息类型
	var jsonMap map[string]interface{}
	if err := json.Unmarshal(messageData, &jsonMap); err != nil {
		return fmt.Errorf("解析JSON失败: %v", err)
	}

	msgType, ok := jsonMap["type"].(string)
	if !ok {
		return errors.New("消息缺少type字段")
	}

	// 根据消息类型处理
	switch msgType {
	case "message":
		var chatMsg user.ChatMessage
		if err := json.Unmarshal(messageData, &chatMsg); err != nil {
			return fmt.Errorf("解析聊天消息失败: %v", err)
		}
		chatMsg.SendID = client.ID

		// 序列化消息
		messageBytes, err := json.Marshal(chatMsg)
		if err != nil {
			return fmt.Errorf("序列化消息失败: %v", err)
		}

		// 发送给接收者
		user.Manager.Mutex.Lock()
		defer user.Manager.Mutex.Unlock()

		if receiverClient, ok := user.Manager.Clients[chatMsg.ReceiveID]; ok {
			if err := writeFramedBytes(receiverClient.Conn, messageBytes); err != nil {
				return fmt.Errorf("发送消息失败: %v", err)
			}
		} else {
			log.Printf("接收者 %s 不在线", chatMsg.ReceiveID)
			// 如果接收者不在线，将消息暂存
			if err := databasetool.CreateUnsendChat(db, chatMsg.SendID, chatMsg.ReceiveID, chatMsg.Content); err != nil {
				return fmt.Errorf("暂存消息失败: %v", err)
			}
		}
	case "changepwd":
		return handleChangePassword(client, []byte(messageStr))
	case "changename":
		return handleChangeName(client, []byte(messageStr))
	case "addfriend":
		return handleAddFriend(client, []byte(messageStr))
	case "acceptfriend":
		return handleAcceptFriend(client, []byte(messageStr))
	default:
		return fmt.Errorf("未知消息类型: %s", msgType)
	}

	return nil
}

func handleAcceptFriend(client *user.Client, messageData []byte) error {
	response := map[string]interface{}{
		"type":       "acceptfriend_response",
		"status":     "success",
		"userid":     client.ID,
		"username":   nil,
		"userstatus": 1,
		"online":     true,
	}

	// 定义好友请求结构
	type AcceptFriendRequest struct {
		Type    string `json:"type"`
		AddName string `json:"addname"`
	}
	// 解析请求
	var acceptFriendRequest AcceptFriendRequest
	if err := json.Unmarshal(messageData, &acceptFriendRequest); err != nil {
		return fmt.Errorf("解析好友请求的回应失败: %v", err)
	}

	// 将发送者ID转换为整数
	senderID, err := strconv.Atoi(client.ID)
	if err != nil {
		return fmt.Errorf("发送者ID转换失败: %v", err)
	}
	// 获取发送者用户信息
	senderUser, err := databasetool.FindUserById(db, senderID)
	if err != nil {
		return fmt.Errorf("查询发送者用户失败: %v", err)
	}
	response["username"] = senderUser.Name
	// 获取接收者用户信息
	receiverUser, err := databasetool.FindUserByName(db, acceptFriendRequest.AddName)
	if err != nil {
		return fmt.Errorf("查询接收者用户失败: %v", err)
	}
	// 接收者ID已经是整数类型(uint)，直接转换为int
	receiverID := int(receiverUser.ID)
	// 添加好友
	if err := databasetool.BeFriend(db, senderID, receiverID); err != nil {
		return fmt.Errorf("添加好友失败: %v", err)
	}
	// 检查好友是否在线
	friendIDStr := fmt.Sprintf("%d", receiverID)
	user.Manager.Mutex.Lock()
	friendClient, online := user.Manager.Clients[friendIDStr]
	user.Manager.Mutex.Unlock()

	responseBytes, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("序列化响应失败: %v", err)
	}
	if online {
		// 发送响应
		if err := writeFramedBytes(friendClient.Conn, responseBytes); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}

		return nil
	} else {
		// 好友不在线，暂存消息
		content := fmt.Sprintf("friend_accepted:%s:%s", client.ID, senderUser.Name)
		if err := databasetool.CreateUnsendChat(db, client.ID, friendIDStr, content); err != nil {
			return fmt.Errorf("暂存好友接受通知失败: %v", err)
		}

		// 发送响应给当前用户
		if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
	}

	return nil
}

func handleAddFriend(client *user.Client, messageData []byte) error {
	response := map[string]interface{}{
		"type":    "addfriend_response",
		"status":  "success",
		"message": "添加好友请求已发送",
	}

	// 定义好友请求结构
	type AddFriendRequest struct {
		Type    string      `json:"type"`
		AddName string      `json:"addname"`
		AddID   interface{} `json:"addid"` // 可能是null或字符串
	}

	// 解析请求数据
	var req AddFriendRequest
	if err := json.Unmarshal(messageData, &req); err != nil {
		response["status"] = "fail"
		response["message"] = "请求格式错误"

		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}

		if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
		return fmt.Errorf("解析好友请求失败: %v", err)
	}

	// 将发送者ID转换为整数
	senderID, err := strconv.Atoi(client.ID)
	if err != nil {
		response["status"] = "fail"
		response["message"] = "服务器错误"

		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}

		if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
		return fmt.Errorf("发送者ID转换失败: %v", err)
	}

	// 获取发送者用户信息
	senderUser, err := databasetool.FindUserById(db, senderID)
	if err != nil {
		response["status"] = "fail"
		response["message"] = "服务器错误"

		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}

		if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
		return fmt.Errorf("查询发送者用户失败: %v", err)
	}

	var friendUser *databasetool.User
	var friendID int

	// 根据AddID或AddName查找好友
	// 检查AddID是否为0、nil或其他无效值
	useNameQuery := false

	// 判断是否应该使用名称查询
	switch v := req.AddID.(type) {
	case float64:
		// JSON中的数字会被解析为float64
		if v == 0 {
			useNameQuery = true
		} else {
			friendID = int(v)
		}
	case int:
		if v == 0 {
			useNameQuery = true
		} else {
			friendID = v
		}
	case string:
		if v == "0" || v == "" {
			useNameQuery = true
		} else {
			var err error
			friendID, err = strconv.Atoi(v)
			if err != nil {
				useNameQuery = true
			}
		}
	default:
		// nil或其他类型，使用名称查询
		useNameQuery = true
	}

	if useNameQuery {
		// 根据名称查询
		friendUser, err = databasetool.FindUserByName(db, req.AddName)
		if err != nil {
			response["status"] = "fail"
			response["message"] = "找不到用户名为 " + req.AddName + " 的用户"

			// 发送响应
			responseBytes, err := json.Marshal(response)
			if err != nil {
				return fmt.Errorf("序列化响应失败: %v", err)
			}

			if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
				return fmt.Errorf("发送响应失败: %v", err)
			}
			return fmt.Errorf("根据名称查询用户失败: %v", err)
		}
		friendID = int(friendUser.ID)
	} else {
		// 根据ID查询
		friendUser, err = databasetool.FindUserById(db, friendID)
		if err != nil {
			response["status"] = "fail"
			response["message"] = "该用户不存在"

			// 发送响应
			responseBytes, err := json.Marshal(response)
			if err != nil {
				return fmt.Errorf("序列化响应失败: %v", err)
			}

			if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
				return fmt.Errorf("发送响应失败: %v", err)
			}
			return fmt.Errorf("查询好友用户失败: %v", err)
		}
	}

	// 创建好友请求消息
	addFriendReq := map[string]interface{}{
		"type":    "addfriend_request",
		"addid":   client.ID,
		"addname": senderUser.Name,
		"status":  1,
		"online":  true,
	}

	// 序列化好友请求消息
	reqBytes, err := json.Marshal(addFriendReq)
	if err != nil {
		response["status"] = "fail"
		response["message"] = "服务器错误"

		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}

		if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
		return fmt.Errorf("序列化好友请求失败: %v", err)
	}

	// 检查好友是否在线
	friendIDStr := fmt.Sprintf("%d", friendID)
	user.Manager.Mutex.Lock()
	friendClient, online := user.Manager.Clients[friendIDStr]
	user.Manager.Mutex.Unlock()

	if online {
		// 好友在线，直接发送请求
		if err := writeFramedBytes(friendClient.Conn, reqBytes); err != nil {
			response["status"] = "fail"
			response["message"] = "发送好友请求失败"

			// 发送响应
			responseBytes, err := json.Marshal(response)
			if err != nil {
				return fmt.Errorf("序列化响应失败: %v", err)
			}

			if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
				return fmt.Errorf("发送响应失败: %v", err)
			}
			return fmt.Errorf("发送好友请求失败: %v", err)
		}
	} else {
		// 好友不在线，暂存消息
		content := fmt.Sprintf("addfriend_request:%s:%s", client.ID, senderUser.Name)
		if err := databasetool.CreateUnsendChat(db, client.ID, friendIDStr, content); err != nil {
			response["status"] = "fail"
			response["message"] = "暂存好友请求失败"

			// 发送响应
			responseBytes, err := json.Marshal(response)
			if err != nil {
				return fmt.Errorf("序列化响应失败: %v", err)
			}

			if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
				return fmt.Errorf("发送响应失败: %v", err)
			}
			return fmt.Errorf("暂存好友请求失败: %v", err)
		}

		log.Printf("用户 %s 的好友请求已暂存，等待用户 %s 上线", client.ID, friendIDStr)
	}

	// 发送成功响应
	responseBytes, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("序列化响应失败: %v", err)
	}

	if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
		return fmt.Errorf("发送响应失败: %v", err)
	}

	log.Printf("用户 %s 发送好友请求给用户 %s", client.ID, friendIDStr)
	return nil
}

// handleChangePassword 处理修改密码请求
func handleChangePassword(client *user.Client, messageData []byte) error {
	response := map[string]interface{}{
		"type":    "changepwd_response",
		"status":  "success",
		"message": "修改密码成功",
	}

	// 定义请求数据结构
	type ChangePasswordRequest struct {
		Type            string `json:"type"`
		CurrentPassword string `json:"oldpwd"`
		NewPassword     string `json:"newpwd"`
	}
	//把client的id转为int
	id, err := strconv.Atoi(client.ID)
	if err != nil {
		response["status"] = "fail"
		response["message"] = "修改密码失败，服务器繁忙"
		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}

		if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
		return fmt.Errorf("修改密码时：id转换失败: %v", err)
	}

	// 解析请求数据
	var req ChangePasswordRequest
	if err := json.Unmarshal(messageData, &req); err != nil {
		response["status"] = "fail"
		response["message"] = "修改密码失败，服务器繁忙"
		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}

		if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
		return fmt.Errorf("解析密码修改请求失败: %v", err)
	}

	userRecord, err := databasetool.FindUserById(db, id)
	if err != nil {
		response["status"] = "fail"
		response["message"] = "修改密码失败，服务器繁忙"
		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}

		if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
		return fmt.Errorf("获取用户信息失败: %v", err)
	}

	// 验证当前密码
	if userRecord.Password != req.CurrentPassword {
		response["status"] = "fail"
		response["message"] = "修改密码失败，原密码不正确"
		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}

		if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
		return errors.New("当前密码不正确")
	}

	// 更新数据库中的密码
	if err := databasetool.ChangePassword(db, id, req.NewPassword); err != nil {
		response["status"] = "fail"
		response["message"] = "修改密码失败，服务器繁忙"
		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}

		if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
		return fmt.Errorf("更新密码失败: %v", err)
	}

	// 发送响应
	responseBytes, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("序列化响应失败: %v", err)
	}

	if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
		return fmt.Errorf("发送响应失败: %v", err)
	}

	log.Printf("用户 %s 修改密码成功", client.ID)
	return nil
}

// handleChangeName 处理修改昵称请求
func handleChangeName(client *user.Client, messageData []byte) error {
	response := map[string]interface{}{
		"type":   "changename_response",
		"status": "success",
	}
	// 定义请求数据结构
	type ChangeNameRequest struct {
		Type    string `json:"type"`
		NewName string `json:"name"`
	}

	//把client的id转为int
	id, err := strconv.Atoi(client.ID)
	if err != nil {
		response["status"] = "fail"
		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}

		if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
		return fmt.Errorf("修改昵称时：id转换失败: %v", err)
	}

	// 解析请求数据
	var req ChangeNameRequest
	if err := json.Unmarshal(messageData, &req); err != nil {
		response["status"] = "fail"
		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}

		if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
		return fmt.Errorf("解析昵称修改请求失败: %v", err)
	}

	// 更新数据库中的昵称
	if err := databasetool.ChangeName(db, id, req.NewName); err != nil {
		response["status"] = "fail"
		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}

		if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
		return fmt.Errorf("昵称修改数据库操作失败: %v", err)
	}
	responseBytes, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("序列化响应失败: %v", err)
	}

	if err := writeFramedBytes(client.Conn, responseBytes); err != nil {
		return fmt.Errorf("发送响应失败: %v", err)
	}

	log.Printf("用户 %s 修改昵称成功", client.ID)
	return nil
}

// handleFileHeaderPacket 处理客户端发送的文件头包(type=3)
func handleFileHeaderPacket(client *user.Client, data []byte) error {
	var header FileHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return fmt.Errorf("解析文件头失败: %v", err)
	}

	fileSize, err := strconv.ParseInt(string(header.Size), 10, 64)
	if err != nil {
		return fmt.Errorf("无效的文件大小: %v", err)
	}

	fileMutex.Lock()
	if oldSession, ok := uploadSessions[client.ID]; ok {
		if oldSession.File != nil {
			_ = oldSession.File.Close()
		}
		if oldSession.FilePath != "" {
			_ = os.Remove(oldSession.FilePath)
		}
		delete(uploadSessions, client.ID)
	}

	fileKey := fmt.Sprintf("%s_%s_%d_%s", header.SendID, header.ReceiveID, time.Now().UnixNano(), header.Filename)
	filePath := filepath.Join(fileStoragePath, fileKey)

	file, err := os.Create(filePath)
	if err != nil {
		fileMutex.Unlock()
		return fmt.Errorf("无法创建文件: %v", err)
	}

	uploadSessions[client.ID] = &UploadSession{
		FileKey:    fileKey,
		FilePath:   filePath,
		File:       file,
		FileSize:   fileSize,
		SenderID:   header.SendID,
		ReceiverID: header.ReceiveID,
		Filename:   header.Filename,
	}
	fileMutex.Unlock()

	user.Manager.Mutex.RLock()
	receiverClient, online := user.Manager.Clients[header.ReceiveID]
	user.Manager.Mutex.RUnlock()

	if online {
		notifyMsg := map[string]interface{}{
			"type":      "file_notify",
			"filename":  header.Filename,
			"size":      header.Size.String(),
			"sendid":    header.SendID,
		}
		notifyBytes, _ := json.Marshal(notifyMsg)
		if err := writeFramedPacket(receiverClient.Conn, 3, notifyBytes); err != nil {
			log.Printf("发送文件通知失败 %s -> %s: %v", client.ID, header.ReceiveID, err)
		}
	}

	return nil
}

// handleFileChunk 处理客户端发送的文件数据包(type=2)
func handleFileChunk(client *user.Client, data []byte) error {
	fileMutex.Lock()
	session, ok := uploadSessions[client.ID]
	fileMutex.Unlock()
	if !ok {
		return fmt.Errorf("未找到文件上传会话: %s", client.ID)
	}

	if session.File != nil {
		if _, err := session.File.Write(data); err != nil {
			return fmt.Errorf("写入临时文件失败: %v", err)
		}
	}

	session.Received += int64(len(data))

	user.Manager.Mutex.RLock()
	receiverClient, online := user.Manager.Clients[session.ReceiverID]
	user.Manager.Mutex.RUnlock()

	if online {
		if err := writeFramedPacket(receiverClient.Conn, 2, data); err != nil {
			log.Printf("转发文件数据失败 %s -> %s: %v", client.ID, session.ReceiverID, err)
		}
	}

	if session.Received >= session.FileSize {
		if session.File != nil {
			_ = session.File.Close()
			session.File = nil
		}

		fileMutex.Lock()
		delete(uploadSessions, client.ID)
		fileMutex.Unlock()

		if !online {
			fileMutex.Lock()
			pendingFiles[session.FileKey] = &FileStorage{
				Filename:   session.Filename,
				FilePath:   session.FilePath,
				FileSize:   session.FileSize,
				Received:   session.Received,
				SendTime:   time.Now(),
				SenderID:   session.SenderID,
				ReceiverID: session.ReceiverID,
			}
			fileMutex.Unlock()

			if err := databasetool.CreateUnsendChat(db, session.SenderID, session.ReceiverID, "file:"+session.FileKey); err != nil {
				return fmt.Errorf("创建离线文件消息失败: %v", err)
			}
			log.Printf("文件 %s 已暂存，等待接收者 %s 上线", session.Filename, session.ReceiverID)
			return nil
		}

		if err := os.Remove(session.FilePath); err != nil {
			log.Printf("删除临时文件失败 %s: %v", session.FilePath, err)
		}
		log.Printf("文件 %s 传输完成", session.Filename)
	}

	return nil
}

// handleFileTransfer 处理文件传输
func handleFileTransfer(client *user.Client, data []byte) error {
	var header FileHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return fmt.Errorf("解析文件头失败: %v", err)
	}

	fileSize, err := strconv.ParseInt(string(header.Size), 10, 64)
	if err != nil {
		return fmt.Errorf("无效的文件大小: %v", err)
	}

	// 检查接收者是否在线
	user.Manager.Mutex.Lock()
	receiver, ok := user.Manager.Clients[header.ReceiveID]
	user.Manager.Mutex.Unlock()

	if ok {
		// 接收者在线，准备直接传输
		return forwardFileToReceiver(client, receiver, &header, fileSize)
	} else {
		// 接收者离线，暂存文件
		return storePendingFile(client, &header, fileSize)
	}
}

// forwardFileToReceiver 转发文件给接收者
func forwardFileToReceiver(sender *user.Client, receiver *user.Client, header *FileHeader, fileSize int64) error {
	// 1. 通知接收者准备接收文件
	notifyMsg := map[string]interface{}{
		"type":      "file_notify",
		"filename":  header.Filename,
		"size":      header.Size.String(),
		"sendid":    header.SendID,
	}
	notifyBytes, _ := json.Marshal(notifyMsg)
	if err := writeFramedBytes(receiver.Conn, notifyBytes); err != nil {
		return fmt.Errorf("发送文件通知失败: %v", err)
	}

	// 2. 从发送者读取文件数据并转发给接收者
	buf := make([]byte, 1024*1024) // 1MB缓冲区
	var received int64 = 0

	for received < fileSize {
		n, err := sender.Conn.Read(buf)
		if err != nil {
			return fmt.Errorf("读取文件数据失败: %v", err)
		}

		// 转发给接收者
		if _, err := receiver.Conn.Write(buf[:n]); err != nil {
			return fmt.Errorf("转发文件数据失败: %v", err)
		}

		received += int64(n)
		log.Printf("文件传输进度: %d/%d (%.2f%%)", received, fileSize, float64(received)*100/float64(fileSize))
	}

	log.Printf("文件 %s 传输完成", header.Filename)
	return nil
}

// storePendingFile 存储待接收文件到文件系统
func storePendingFile(sender *user.Client, header *FileHeader, fileSize int64) error {
	fileMutex.Lock()
	defer fileMutex.Unlock()

	// 生成唯一文件名防止冲突
	fileKey := fmt.Sprintf("%s_%s_%d_%s",
		header.SendID,
		header.ReceiveID,
		time.Now().UnixNano(),
		header.Filename)
	filePath := filepath.Join(fileStoragePath, fileKey)

	// 创建文件
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("无法创建文件: %v", err)
	}
	defer file.Close()

	// 读取并存储文件数据
	buf := make([]byte, 1024*1024) // 1MB缓冲区
	var received int64 = 0

	for received < fileSize {
		n, err := sender.Conn.Read(buf)
		if err != nil {
			os.Remove(filePath) // 删除不完整的文件
			return fmt.Errorf("读取文件数据失败: %v", err)
		}

		if _, err := file.Write(buf[:n]); err != nil {
			os.Remove(filePath) // 删除不完整的文件
			return fmt.Errorf("写入文件失败: %v", err)
		}

		received += int64(n)
	}

	// 保存文件信息
	pendingFiles[fileKey] = &FileStorage{
		Filename:   header.Filename,
		FilePath:   filePath,
		FileSize:   fileSize,
		Received:   received,
		SendTime:   time.Now(),
		SenderID:   header.SendID,
		ReceiverID: header.ReceiveID,
	}

	log.Printf("文件 %s 已暂存到 %s，等待接收者 %s 上线", header.Filename, filePath, header.ReceiveID)
	databasetool.CreateUnsendChat(db, header.SendID, header.ReceiveID, "file:"+fileKey)
	return nil
}

// checkPendingFiles 发送待接收文件
func checkPendingFiles(client *user.Client, fkey string) error {
	fileMutex.Lock()
	file, ok := pendingFiles[fkey]
	if !ok || file.ReceiverID != client.ID {
		fileMutex.Unlock()
		return nil
	}
	fileMutex.Unlock()

	notifyMsg := map[string]interface{}{
		"type":      "file_notify",
		"filename":  file.Filename,
		"size":      strconv.FormatInt(file.FileSize, 10),
		"sendid":    file.SenderID,
	}
	notifyBytes, _ := json.Marshal(notifyMsg)
	if err := writeFramedPacket(client.Conn, 3, notifyBytes); err != nil {
		return fmt.Errorf("发送文件通知失败: %v", err)
	}

	fileData, err := os.Open(file.FilePath)
	if err != nil {
		return fmt.Errorf("无法打开存储的文件: %v", err)
	}
	defer fileData.Close()

	buf := make([]byte, 1024*1024)
	for {
		n, err := fileData.Read(buf)
		if err != nil && err != io.EOF {
			return fmt.Errorf("读取存储文件失败: %v", err)
		}
		if n == 0 {
			break
		}

		if err := writeFramedPacket(client.Conn, 2, buf[:n]); err != nil {
			return fmt.Errorf("发送文件数据失败: %v", err)
		}
	}

	fileMutex.Lock()
	delete(pendingFiles, fkey)
	fileMutex.Unlock()

	if err := os.Remove(file.FilePath); err != nil {
		log.Printf("删除已发送的离线文件失败 %s: %v", file.FilePath, err)
	}

	log.Printf("离线文件 %s 已发送给 %s", file.Filename, client.ID)
	return nil
}

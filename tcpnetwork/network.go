
package tcpnetwork

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
	"bytes"
	"os"
    "path/filepath"
    "io"
	"sync"
	"connection_server_linux/databasetool"
	"connection_server_linux/friendupdate"
	"connection_server_linux/user"
	"gorm.io/gorm"
	"strconv"
)

// FileHeader 文件头信息
type FileHeader struct {
    Type     string `json:"type"`      // 固定为"file_transfer"
    Filename string `json:"filename"`  // 文件名
    Size     string `json:"size"`      // 文件大小(字符串)
    SendID   string `json:"sendid"`    // 发送者ID
    ReceiveID string `json:"receiveid"` // 接收者ID
}

// FileStorage 文件存储结构
type FileStorage struct {
    Filename   string
    FilePath   string    // 添加文件路径字段
    FileSize   int64
    Received   int64
    SendTime   time.Time
    SenderID   string
    ReceiverID string
}

var (
    pendingFiles = make(map[string]*FileStorage) // 待接收文件缓存
    fileMutex    sync.Mutex                     // 文件操作互斥锁
	fileStoragePath = "/root/connection_server/connection_server_linux/file_storage/" // 文件存储目录
)

var (
	TcpAddr *net.TCPAddr // 全局TCP地址
	db      *gorm.DB // 添加全局db引用
)

func InitDBConnection(globalDB *gorm.DB) {
	db = globalDB
}

// LoginRequest 客户端登录请求结构
type LoginRequest struct {
	Type     string `json:"type"`      // 消息类型，固定为"login"
	Username   string `json:"name"`   // 用户ID
	Password string `json:"pwd"`  // 密码
}

// LoginResponse 登录响应结构
type LoginResponse struct {
	Type    string `json:"type"`     // 消息类型，固定为"login_response"
	Success bool   `json:"success"`  // 是否成功
	Message string `json:"message"`  // 返回消息
}

// handleLogin 处理登录验证
func handleLogin(conn net.Conn) (*user.Client, error) {
	// 设置登录超时（30秒）
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	defer conn.SetReadDeadline(time.Time{}) // 处理完成后重置超时

	// 读取登录数据
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("读取登录数据失败: %v", err)
	}

	// 清理接收到的数据
    rawData := buf[:n]
    // 查找第一个 '{'，丢弃之前的所有字符
    startIdx := bytes.IndexByte(rawData, '{')
    if startIdx == -1 {
        return nil, fmt.Errorf("无效的登录数据: 缺少JSON起始标记")
    }
    cleanData := rawData[startIdx:]

    fmt.Println("清理后的登录数据:", string(cleanData))
    
    var loginReq LoginRequest
    if err := json.Unmarshal(cleanData, &loginReq); err != nil {
        return nil, fmt.Errorf("解析登录数据失败: %v", err)
    }

	// 验证消息类型
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

	// 验证密码
	if userRecord.Password != loginReq.Password {
		sendLoginResponse(conn, false, "用户名或密码错误")
		return nil, errors.New("用户名或密码错误")
	}

	// 更新用户状态为在线
	userRecord.Status = 1 // 1表示在线
	userRecord.Ip = conn.RemoteAddr().(*net.TCPAddr).IP.String()
	userRecord.LeaveTime = time.Now()
	if err := db.Save(&userRecord).Error; err != nil {
		sendLoginResponse(conn, false, "服务器错误")
		return nil, fmt.Errorf("更新用户状态失败: %v", err)
	}

	// 创建客户端对象
	client := &user.Client{
		Conn:        conn,
		ID:          fmt.Sprintf("%d",userRecord.ID),
		IP:          conn.RemoteAddr().(*net.TCPAddr).IP.String(),
		ConnectTime: time.Now(),
		LastActive:  time.Now(),
		Friends:     make([]user.FriendInfo, 0),
	}

	// 添加到客户端管理器
	user.Manager.Mutex.Lock()
	user.Manager.Clients[client.ID] = client
	user.Manager.Mutex.Unlock()

	// 发送登录成功响应
	sendLoginResponse(conn, true, "id:"+fmt.Sprintf("%d",userRecord.ID))
	return client, nil
}

// sendLoginResponse 发送登录响应
func sendLoginResponse(conn net.Conn, success bool, message string) {
	response := LoginResponse{
		Type:    "login_response",
		Success: success,
		Message: message,
	}
	responseBytes, _ := json.Marshal(response)
	conn.Write(append(responseBytes, '\n'))
}

// HandleConnection 处理新TCP连接
func HandleConnection(conn net.Conn) {
	defer conn.Close()

	// 1. 处理登录
	client, err := handleLogin(conn)
	if err != nil {
		log.Printf("登录失败: %v", err)
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
		chatMsg := user.ChatMessage{
			Type:    "message",
			SendID:  chat.Sendid,
			ReceiveID: chat.Reciveid,
			Content: chat.Content,
			SendTime: chat.SendTime.String(),
		}
		if strings.HasPrefix(chatMsg.Content, "file:") {
			filekey := strings.TrimPrefix(chatMsg.Content, "file:")
			if err := checkPendingFiles(client,filekey); err != nil {
				log.Printf("发送待接收文件失败 %s: %v", client.ID, err)
			}
			continue // 跳过文件消息
		}
		msgBytes, err := json.Marshal(chatMsg)
		if err!= nil {
			log.Printf("序列化消息失败 %s: %v", client.ID, err)
			continue
		}
		if _, err := client.Conn.Write(append(msgBytes, '\n')); err!= nil {
			log.Printf("发送消息失败 %s: %v", client.ID, err)
			continue
		}

		// 从数据库中删除已发送的消息
		if err := databasetool.DeleteUnsendChat(db, chat.Logid); err!= nil {
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

	if _, err := client.Conn.Write(append(msgBytes, '\n')); err != nil {
		return fmt.Errorf("发送好友列表失败: %v", err)
	}

	return nil
}

// messageLoop 消息处理循环
func messageLoop(client *user.Client) {
	buf := make([]byte, 4096)
	for {
		n, err := client.Conn.Read(buf)
		if err != nil {
			log.Printf("客户端 %s 断开连接: %v", client.ID, err)
			cleanupClient(client)
			return
		}

		client.LastActive = time.Now()
		if err := handleMessage(client, buf[:n]); err != nil {
			log.Printf("处理消息失败 %s: %v", client.ID, err)
		}
	}
}

// cleanupClient 清理客户端资源
func cleanupClient(client *user.Client) {

	if err := db.Model(&databasetool.User{}).Where("id = ?", client.ID).Updates(map[string]interface{}{
		"Status":     0, // 0表示离线
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
	
	// 清理消息数据（提取有效JSON部分）
	cleanData := make([]byte, 0, len(messageData))
	foundOpenBrace := false
	braceCount := 0

	for _, b := range messageData {
		if b == '{' {
			foundOpenBrace = true
			braceCount++
		}
		if foundOpenBrace {
			cleanData = append(cleanData, b)
			if b == '}' {
				braceCount--
				if braceCount == 0 {
					break
				}
			}
		}
	}

	messageStr := strings.TrimSpace(string(cleanData))
	if len(messageStr) == 0 {
		return errors.New("空消息")
	}

	// 解析消息类型
	var jsonMap map[string]interface{}
	if err := json.Unmarshal([]byte(messageStr), &jsonMap); err != nil {
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
		if err := json.Unmarshal([]byte(messageStr), &chatMsg); err != nil {
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
			if _, err := receiverClient.Conn.Write(append(messageBytes, '\n')); err != nil {
				return fmt.Errorf("发送消息失败: %v", err)
			}
		} else {
			log.Printf("接收者 %s 不在线", chatMsg.ReceiveID)
			// 如果接收者不在线，将消息暂存
			if err := databasetool.CreateUnsendChat(db,chatMsg.SendID,chatMsg.ReceiveID,chatMsg.Content); err!= nil {
				return fmt.Errorf("暂存消息失败: %v", err)
			}
		}
	case "file_transfer":
		// 处理文件传输消息
		return handleFileTransfer(client, []byte(messageStr))
	case "changepwd":
		return handleChangePassword(client, []byte(messageStr))
	case "changename":
		return handleChangeName(client, []byte(messageStr))
	default:
		return fmt.Errorf("未知消息类型: %s", msgType)
	}

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
	if err!= nil {
		response["status"] = "fail"
		response["message"] = "修改密码失败，服务器繁忙"
		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}
	
		if _, err := client.Conn.Write(append(responseBytes, '\n')); err != nil {
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
	
		if _, err := client.Conn.Write(append(responseBytes, '\n')); err != nil {
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
	
		if _, err := client.Conn.Write(append(responseBytes, '\n')); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
        return fmt.Errorf("获取用户信息失败: %v", err)
    }

    // 验证当前密码 
    if userRecord.Password!=req.CurrentPassword {
		response["status"] = "fail"
		response["message"] = "修改密码失败，原密码不正确"
		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}
	
		if _, err := client.Conn.Write(append(responseBytes, '\n')); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
        return errors.New("当前密码不正确")
    }

    // 更新数据库中的密码
    if err := databasetool.ChangePassword(db,id,req.NewPassword); err != nil {
		response["status"] = "fail"
		response["message"] = "修改密码失败，服务器繁忙"
		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}
	
		if _, err := client.Conn.Write(append(responseBytes, '\n')); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
        return fmt.Errorf("更新密码失败: %v", err)
    }

    // 发送响应
    responseBytes, err := json.Marshal(response)
    if err != nil {
        return fmt.Errorf("序列化响应失败: %v", err)
    }

    if _, err := client.Conn.Write(append(responseBytes, '\n')); err != nil {
        return fmt.Errorf("发送响应失败: %v", err)
    }

    log.Printf("用户 %s 修改密码成功", client.ID)
    return nil
}
// handleChangeName 处理修改昵称请求
func handleChangeName(client *user.Client, messageData []byte) error {
    response := map[string]interface{}{
        "type":    "changename_response",
        "status":  "success",
    }
	// 定义请求数据结构
    type ChangeNameRequest struct {
        Type     string `json:"type"`
        NewName  string `json:"name"`
    }

	//把client的id转为int
	id, err := strconv.Atoi(client.ID)
	if err!= nil {
		response["status"] = "fail"
		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}
	
		if _, err := client.Conn.Write(append(responseBytes, '\n')); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
        return fmt.Errorf("修改昵称时：id转换失败: %v", err)
    }

    // 解析请求数据
    var req ChangeNameRequest
    if err := json.Unmarshal(messageData, &req); err!= nil {
    	response["status"] = "fail"
		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}
	
		if _, err := client.Conn.Write(append(responseBytes, '\n')); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
        return fmt.Errorf("解析昵称修改请求失败: %v", err)
    }

    // 更新数据库中的昵称
    if err := databasetool.ChangeName(db, id, req.NewName); err!= nil {
    	response["status"] = "fail"
		// 发送响应
		responseBytes, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("序列化响应失败: %v", err)
		}
	
		if _, err := client.Conn.Write(append(responseBytes, '\n')); err != nil {
			return fmt.Errorf("发送响应失败: %v", err)
		}
        return fmt.Errorf("昵称修改数据库操作失败: %v", err)
    }
	responseBytes, err := json.Marshal(response)
    if err != nil {
        return fmt.Errorf("序列化响应失败: %v", err)
    }

    if _, err := client.Conn.Write(append(responseBytes, '\n')); err != nil {
        return fmt.Errorf("发送响应失败: %v", err)
    }

    log.Printf("用户 %s 修改昵称成功", client.ID)
    return nil
}

// handleFileTransfer 处理文件传输
func handleFileTransfer(client *user.Client, data []byte) error {
    var header FileHeader
    if err := json.Unmarshal(data, &header); err != nil {
        return fmt.Errorf("解析文件头失败: %v", err)
    }

    fileSize, err := strconv.ParseInt(header.Size, 10, 64)
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
        "size":      header.Size,
        "sender_id": header.SendID,
    }
    notifyBytes, _ := json.Marshal(notifyMsg)
    if _, err := receiver.Conn.Write(append(notifyBytes, '\n')); err != nil {
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
	databasetool.CreateUnsendChat(db,header.SendID,header.ReceiveID,"file:"+fileKey)
    return nil
}
// checkPendingFiles 发送待接收文件
func checkPendingFiles(client *user.Client,fkey string) error {
    fileMutex.Lock()
    defer fileMutex.Unlock()

    for key, file := range pendingFiles {
        if file.ReceiverID == client.ID && key==fkey {
            // 通知客户端准备接收文件
            notifyMsg := map[string]interface{}{
                "type":      "file_notify",
                "filename":  file.Filename,
                "size":      strconv.FormatInt(file.FileSize, 10),
                "sender_id": file.SenderID,
            }
            notifyBytes, _ := json.Marshal(notifyMsg)
            if _, err := client.Conn.Write(append(notifyBytes, '\n')); err != nil {
                return fmt.Errorf("发送文件通知失败: %v", err)
            }

            // 打开存储的文件
            fileData, err := os.Open(file.FilePath)
            if err != nil {
                return fmt.Errorf("无法打开存储的文件: %v", err)
            }
            defer fileData.Close()

            // 分块发送文件内容
            buf := make([]byte, 1024*1024) // 1MB缓冲区
            for {
                n, err := fileData.Read(buf)
                if err != nil && err != io.EOF {
                    return fmt.Errorf("读取存储文件失败: %v", err)
                }
                if n == 0 {
                    break
                }

                if _, err := client.Conn.Write(buf[:n]); err != nil {
                    return fmt.Errorf("发送文件数据失败: %v", err)
                }
            }

            // 清理资源
            fileData.Close()
            os.Remove(file.FilePath) // 删除已发送的文件
            delete(pendingFiles, key)
            
            log.Printf("离线文件 %s 已发送给 %s", file.Filename, client.ID)
        }
    }

    return nil
}
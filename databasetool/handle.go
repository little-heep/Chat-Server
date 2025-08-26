package databasetool
import (
    "gorm.io/driver/mysql"
    "gorm.io/gorm"
    "log"
	"time"
    "fmt"
)

func InitDB() *gorm.DB {
    dsn := "newuser:password@tcp(localhost:3306)/communication?charset=utf8mb4&parseTime=True&loc=Local"
    db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("failed to connect database")
    }

    // 确认连接成功
    sqlDB, err := db.DB()
    if err != nil {
        log.Fatal(err)
    }
    
    // 设置连接池参数
    sqlDB.SetMaxIdleConns(10)
    sqlDB.SetMaxOpenConns(100)
    sqlDB.SetConnMaxLifetime(time.Hour)
    
    return db
}

// 注册用户
func RegisterUser(db *gorm.DB, name string, password string, ip string) error {
    now := time.Now()
    relationBytes := make([]byte, 8)
    user := &User{Name: name, Password: password, Ip: ip, Relation: relationBytes, RegisterTime: now, Status: 1}
    result := db.Create(user) // 通过数据的指针来创建
    return result.Error
}

//通过用户名查找用户
func FindUserByName(db *gorm.DB, name string) (*User, error) {
    var user User
    result := db.First(&user, "Name = ?", name) // 通过唯一键查询
    if result.Error != nil {
        return nil, result.Error
    }
    return &user, nil
}

//通过用户id查找用户
func FindUserById(db *gorm.DB, id int) (*User, error) {
    var user User
    result := db.First(&user, "Id = ?", id) // 通过主键查询
    if result.Error != nil {
        return nil, result.Error
    }
    return &user, nil
}

//删除用户
func DeleteUser(db *gorm.DB, id int) error {
    var user User
    result := db.First(&user, "Id = ?", id) // 通过唯一键查询
    if result.Error != nil {
        return result.Error
    }
    result = db.Delete(&user)
    return result.Error
}

//用户离线
func UserOffline(db *gorm.DB, id int) error {
    now := time.Now() //离线时间
    
    //更新用户的登录状态及离线时间
    result := db.Model(&User{}).Where("Id = ?", id).Updates(map[string]interface{}{"Status": 0, "LeaveTime": now})
    return result.Error
}

//用户上线
func UserOnline(db *gorm.DB, id int,ip string) error {
    //更新用户登录状态及IP地址
    result := db.Model(&User{}).Where("Id =?", id).Updates(map[string]interface{}{"Status": 1,"Ip": ip})
    return result.Error
}

//修改用户密码
func ChangePassword(db *gorm.DB, id int, password string) error {
    //更新用户密码
    result := db.Model(&User{}).Where("Id = ?", id).Update("Password", password)
    return result.Error
}

//修改用户名
func ChangeName(db *gorm.DB, id int, name string) error {
   result := db.Model(&User{}).Where("Id =?", id).Update("Name", name)
   return result.Error 
}

//将两个用户变为好友
// SetRelationBit 设置用户关系位
// id: 用户ID
// targetID: 目标用户ID
// relation: 关系字节数组
// relationValue: 关系值（0=无关系, 1=好友, 2=待确认, 3=拉黑）
// 返回: 修改后的关系字节数组
func SetRelationBit(data []byte, groupNum int, value int) ([]byte, error) {
    // 创建数据的副本，避免修改原数组
    result := make([]byte, len(data))
    copy(result, data)
    
    // 检查参数有效性
    if groupNum < 1 {
        return nil, fmt.Errorf("group number must be at least 1")
    }
    
    if value < 0 || value > 3 {
        return nil, fmt.Errorf("value must be between 0 and 3")
    }
    
    // 计算组号对应的字节位置（从右向左）
    totalGroups := len(result) * 4 // 每个字节包含4个组
    if groupNum > totalGroups {
        return nil, fmt.Errorf("group number %d exceeds maximum available groups %d", groupNum, totalGroups)
    }
    
    // 计算组在字节数组中的位置（从右向左）
    byteIndex := len(result) - 1 - (groupNum-1)/4
    if byteIndex < 0 || byteIndex >= len(result) {
        return nil, fmt.Errorf("calculated byte index out of range")
    }
    
    // 计算组在字节中的位置（从右向左，每2位一组）
    bitPos := ((groupNum - 1) % 4) * 2
    
    // 清除原来的值（将对应的2位清零）
    mask := ^(byte(3) << bitPos) // 创建掩码：将对应位置清零
    result[byteIndex] &= mask
    
    // 设置新的值
    result[byteIndex] |= byte(value) << bitPos
    
    return result, nil
}

// BeFriend 将两个用户设置为好友关系
func BeFriend(db *gorm.DB, id1 int, id2 int) error {
    // 获取用户1的信息
    user1, err := FindUserById(db, id1)
    if err != nil {
        return fmt.Errorf("查询用户1失败: %v", err)
    }
    
    // 获取用户2的信息
    user2, err := FindUserById(db, id2)
    if err != nil {
        return fmt.Errorf("查询用户2失败: %v", err)
    }
    
    // 修改用户1的关系字节，将用户2设置为好友
    user1.Relation,_ = SetRelationBit(user1.Relation, id2, 1) // 1表示好友关系
    
    // 修改用户2的关系字节，将用户1设置为好友
    user2.Relation,_ = SetRelationBit(user2.Relation, id1, 1) // 1表示好友关系
    
    // 更新用户1的关系到数据库
    if err := UpdateRelation(db, id1, user1.Relation); err != nil {
        return fmt.Errorf("更新用户1关系失败: %v", err)
    }
    
    // 更新用户2的关系到数据库
    if err := UpdateRelation(db, id2, user2.Relation); err != nil {
        return fmt.Errorf("更新用户2关系失败: %v", err)
    }
    
    return nil
}

//更新好友关系
func UpdateRelation(db *gorm.DB, id int, relation []byte) error {
    result := db.Model(&User{}).Where("Id =?", id).Update("Relation", relation)
    return result.Error 
}


// 根据reciveid查询记录并按时间排序
func GetUnsendChatsByReciveID(db *gorm.DB, reciveid string) ([]Unsendchat, error) {
	var chats []Unsendchat
	result := db.Where("reciveid = ?", reciveid).
		Order("sendTime desc").
		Find(&chats)
	return chats, result.Error
}

// 删除指定记录
func DeleteUnsendChat(db *gorm.DB, logid int) error {
	result := db.Delete(&Unsendchat{}, logid)
	return result.Error
}

// 添加新记录
func CreateUnsendChat(db *gorm.DB, sendid, reciveid string, content string) (error) {
	newChat := Unsendchat{
		Sendid:    sendid,
		Reciveid:  reciveid,
		Content:   content,
		SendTime:  time.Now(),
	}
	result := db.Create(&newChat)
	return result.Error
}
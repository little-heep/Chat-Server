package databasetool
import (
    "gorm.io/driver/mysql"
    "gorm.io/gorm"
    "log"
	"time"
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
    result := db.First(&user, "name = ?", name) // 通过唯一键查询
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
package databasetool

import (
    //"gorm.io/gorm"
    "time"
)

type User struct {
    ID           uint   `gorm:"column:Id;primaryKey;autoIncrement"` // 主键，自动递增
    Name         string `gorm:"column:Name;type:varchar(30);not null"` // 名称，长度为30，不允许为空
    Password     string `gorm:"column:Password;type:varchar(20);not null"` // 密码，长度为20，不允许为空
    Ip           string `gorm:"column:Ip;type:varchar(20);not null"` // IP地址，长度为20，不允许为空
    Relation     []byte `gorm:"column:Relation;type:bit(64);not null"` // 关系，类型为bit(64)，不允许为空
    RegisterTime time.Time `gorm:"column:RegisterTime;type:datetime;not null"` // 注册时间，不允许为空
    LeaveTime    time.Time `gorm:"column:LeaveTime;type:datetime"` // 离开时间，允许为空
    Status       int   `gorm:"column:Status;type:int(1);not null"` // 状态，类型为bit(1)，不允许为空
}
func (User) TableName() string {
    return "User" // 指定表名为User
}

//未发送消息的表
type Unsendchat struct {
	Logid     int       `gorm:"column:logid;primaryKey;autoIncrement"`// 主键
	Sendid    string       `gorm:"column:sendid;not null"`               // 发送者ID，不允许为空
	Reciveid  string       `gorm:"column:reciveid;not null"`             // 接收者ID，不允许为空
	Content   string    `gorm:"column:content;type:text;not null"`    // 消息内容，类型为text，不允许为空
	SendTime  time.Time `gorm:"column:sendTime;type:datetime;not null"` // 发送时间，不允许为空
}

func (Unsendchat) TableName() string {
	return "Unsendchat" // 指定表名为Unsendchat
}
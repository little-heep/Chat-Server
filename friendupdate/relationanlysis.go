package friendupdate
//friends[a]表示与a的关系，0表示没有好友，1表示好友，2表示已经发送好友请求但未同意，3表示拉黑
//从右到左每两位为一个好友，00表示没有好友，01表示好友，10表示已经发送好友请求但未同意，11表示拉黑
func AnalyzeRelationByte(relation []byte) []int {
    totalBits := len(relation) * 8       // 总位数
    maxUserID := totalBits / 2           // 最大用户ID数量
    friends := make([]int, maxUserID+1)  // 索引从 0 到 maxUserID，实际使用从 1 开始
    friends=append(friends,0)//预留第0个位置放自己的ID

    for i := 0; i < totalBits; i += 2 {
        // 第一个 bit 的位置（从右往左）
        byteIndex1 := (i) / 8
        //bitIndex1 := (i) % 8 // 注意：这里是从最低位开始（右边）
        bitIndex1 := 7 - ((i) % 8 )// 注意：这里是从最高位开始（左边）
        b1 := (relation[byteIndex1] >> bitIndex1) & 1

        // 第二个 bit 的位置
        byteIndex2 := (i + 1) / 8
        //bitIndex2 := (i + 1) % 8
        bitIndex2 := 7 - ((i + 1) % 8 )
        b2 := (relation[byteIndex2] >> bitIndex2) & 1

        userID := (i / 2) + 1 // 第一组对应用户ID=1

        if userID >= len(friends) {
            continue // 避免越界
        }

        if b1 == 0 && b2 == 0 {
            friends=append(friends,0) // 无好友
        } else if b1 == 0 && b2 == 1 {
            friends=append(friends,1) // 好友 
        } else if b1 == 1 && b2 == 0 {
            friends=append(friends,2) // 请求中
        } else if b1 == 1 && b2 == 1 {
            //friends[userID] = 3 // 拉黑
            friends=append(friends,3) // 拉黑
        }
    }

    //反转位置
    n:=len(friends)
    start := 1
    end := n - 1

    // 反转数组的后半部分
    for start < end {
        friends[start], friends[end] = friends[end], friends[start]
        start++
        end--
    }

    return friends
}

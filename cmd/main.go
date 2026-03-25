package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/orimono/ito" // 确保你的 go.work 或 replace 已配置好
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func handleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		return
	}
	defer conn.Close()

	fmt.Println("针 (Hari) 已连接！等待 JoinPacket...")

	for {
		// 读取消息
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Read error: %v", err)
			break
		}

		// 尝试用 Ito 协议解码
		var joinPkg ito.JoinPacket
		if err := json.Unmarshal(message, &joinPkg); err != nil {
			log.Printf("解析 Ito 数据失败: %v", err)
			continue
		}

		// 打印纳管信息
		fmt.Printf("成功识别节点 [%s]\n", joinPkg.NodeID)
		fmt.Printf("主机名: %s, 架构: %s/%s\n", joinPkg.Hostname, joinPkg.OS, joinPkg.Arch)
		fmt.Printf("已载任务数: %d\n", len(joinPkg.TaskManifest))

		// 回复一个简单的确认（测试用）
		response := map[string]string{"status": "accepted", "msg": "Musubi 已经感知到你"}
		resBytes, _ := json.Marshal(response)
		conn.WriteMessage(websocket.TextMessage, resBytes)
	}
}

func main() {
	http.HandleFunc("/ws", handleConnection)
	fmt.Println("Musubi 测试服务器启动在 :8080/ws ...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

package httpservers

import (
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
)

var WsChannel chan string

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func startWebsocketServer() {
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		upgrader.CheckOrigin = func(r *http.Request) bool { return true }
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			fmt.Println(err)
			return
		}
		defer ws.Close()

		// discard received messages
		go func(c *websocket.Conn) {
			for {
				if _, _, err := c.NextReader(); err != nil {
					c.Close()
					break
				}
			}
		}(ws)
		for {
			ws.WriteMessage(1, []byte(<-WsChannel))
		}
	})

	_ = http.ListenAndServe(":8000", nil)
}

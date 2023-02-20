package httpservers

import (
	"fmt"
	"github.com/OliveTin/OliveTin/internal/config"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"net/http"
)

var WsChannel chan string

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func startWebsocketServer(cfg *config.Config) {

	log.WithFields(log.Fields{
		"address": cfg.ListenAddressWebSocket,
	}).Info("Starting WebSocket server")

	http.HandleFunc("/cli", func(w http.ResponseWriter, r *http.Request) {
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
			webSocketWriteErr := ws.WriteMessage(1, []byte(<-WsChannel))
			if webSocketWriteErr != nil {
				log.Error(webSocketWriteErr)
				return
			}
		}
	})
	_ = http.ListenAndServe(cfg.ListenAddressWebSocket, nil)
}

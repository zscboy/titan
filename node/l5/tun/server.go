package tun

// var (
// 	upgrader = websocket.Upgrader{} // use default options
// 	wsIndex  = 0
// 	// accountMap    = make(map[string]*Account)
// 	dnsServerAddr *net.UDPAddr
// )

// func WSHandler(w http.ResponseWriter, r *http.Request) {
// 	c, err := upgrader.Upgrade(w, r, nil)
// 	if err != nil {
// 		log.Print("upgrade:", err)
// 		return
// 	}
// 	defer c.Close()

// 	var uuid = r.URL.Query().Get("uuid")
// 	if uuid == "" {
// 		log.Println("need uuid!")
// 		return
// 	}

// 	account, ok := accountMap[uuid]
// 	if !ok {
// 		log.Println("no account found for uuid:", uuid)
// 		return
// 	}

// 	account.acceptWebsocket(c)
// }

// func keepalive() {
// 	for {
// 		time.Sleep(time.Second * 30)

// 		for _, a := range accountMap {
// 			a.keepalive()
// 		}
// 	}
// }

// func setupBuiltinAccount() {

// 	uuids := []string{
// 		"ee80e87b-fc41-4e59-a722-7c3fee039cb4",
// 		"f6000866-1b89-4ab4-b1ce-6b7625b8259a"}

// 	for _, u := range uuids {
// 		accountMap[u] = newAccount(u)
// 	}
// }

// CreateHTTPServer start http server
// func CreateHTTPServer(listenAddr string, wsPath string) {
// 	// setupBuiltinAccount()

// 	var err error
// 	dnsServerAddr, err = net.ResolveUDPAddr("udp", "8.8.8.8:53")
// 	if err != nil {
// 		log.Fatal("resolve dns server address failed:", err)
// 	}

// 	go keepalive()
// 	http.HandleFunc(wsPath, wsHandler)
// 	log.Printf("server listen at:%s, path:%s", listenAddr, wsPath)
// 	log.Fatal(http.ListenAndServe(listenAddr, nil))
// }

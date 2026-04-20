package adapter

import "github.com/perfect-panel/server/internal/model/node"

func testNodes() []*node.Node {
	return []*node.Node{
		{
			Id:       1,
			Name:     "Trojan Node",
			Tags:     "premium,hk",
			Port:     443,
			Address:  "node.example.com",
			ServerId: 10,
			Protocol: "trojan",
			Sort:     2,
			Server: &node.Server{
				Id: 10,
				Protocols: `[
					{
						"type":"trojan",
						"listener_key":"trojan-main",
						"port":443,
						"enable":true,
						"security":"tls",
						"sni":"edge.example.com",
						"allow_insecure":true,
						"fingerprint":"chrome"
					},
					{
						"type":"vmess",
						"listener_key":"vmess-unused",
						"port":8443,
						"enable":true
					}
				]`,
			},
		},
		{
			Id:       2,
			Name:     "Broken Node",
			Port:     80,
			Address:  "broken.example.com",
			ServerId: 11,
			Protocol: "vless",
			Sort:     3,
			Server: &node.Server{
				Id:        11,
				Protocols: `{"invalid":true}`,
			},
		},
		{
			Id:       3,
			Name:     "Missing Server",
			Port:     1234,
			Address:  "missing.example.com",
			ServerId: 12,
			Protocol: "shadowsocks",
			Sort:     4,
		},
	}
}

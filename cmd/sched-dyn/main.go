package main

import (
	"fmt"
	"sort"
)

type Node struct {
	Id   int
	Prio int
}

const (
	CMD_ADD   = "add"
	CMD_SET   = "set"
	CMD_DEL   = "del"
	CMD_EXIT  = "exit"
	CMD_PRINT = "print"
)

func main() {
	nodes := make([]*Node, 0)

	sort := func() {
		sort.Slice(nodes, func(i, j int) bool {
			return nodes[i].Prio <= nodes[j].Prio
		})
	}

	print := func() {
		for rank, node := range nodes {
			fmt.Printf("[%d] ID=%d, Prio=%d\n", rank, node.Id, node.Prio)
		}
		fmt.Println("--------------------------------")
	}

	update := func(id, newPrio int) {
		for _, node := range nodes {
			if node.Id == id {
				node.Prio = newPrio
				break
			}
		}
	}

	for {
		var cmd string
		var idx int
		fmt.Scan(&cmd)
		switch cmd {
		case CMD_ADD, "a":
			newNodeId := len(nodes)
			node := Node{Id: newNodeId, Prio: 0}
			nodes = append(nodes, &node)
			fmt.Printf("added node %d\n", newNodeId)
		case CMD_SET, "s":
			var newVal int
			fmt.Scanln(&idx, &newVal)
			update(idx, newVal)
			fmt.Printf("set node %d prio to %d\n", nodes[idx].Id, newVal)
		case CMD_DEL, "d":
			fmt.Scanln(&idx)
			newNodes := make([]*Node, 0)
			for _, node := range nodes {
				if node.Id != idx {
					newNodes = append(newNodes, node)
				}
			}
			nodes = newNodes
			fmt.Printf("delete node %d\n", idx)
		case CMD_PRINT, "p":
			print()
		case CMD_EXIT, "e":
			fmt.Println("bye!")
			return
		default:
			panic(fmt.Sprintf("unknown command: %s", cmd))
		}
		sort()
		print()
	}
}

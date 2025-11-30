package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
	pkgnodereg "example.com/rbmq-demo/pkg/nodereg"
	pkgrbmqrpc "example.com/rbmq-demo/pkg/rpc"
	pkgsimpleping "example.com/rbmq-demo/pkg/simpleping"
	amqp "github.com/rabbitmq/amqp091-go"
)

var addr = flag.String("addr", "localhost:8080", "http service address")
var path = flag.String("path", "/ws", "websocket path")
var nodeName = flag.String("node-name", "agent-1", "node name")
var logEchoReplies = flag.Bool("log-echo-replies", false, "log echo replies")
var rabbitMQBrokerURL = flag.String("rabbitmq-broker-url", "amqp://localhost:5672/", "RabbitMQ broker URL")
var reconnectAtDisconnect = flag.Bool("reconnect-at-disconnect", false, "reconnect at disconnect")

func handleTask(ctx context.Context, taskMsg *amqp.Delivery, updatesCh chan<- pkgrbmqrpc.TaskUpdate) {

	var pingCfg pkgsimpleping.PingConfiguration
	err := json.Unmarshal(taskMsg.Body, &pingCfg)
	if err != nil {
		updatesCh <- pkgrbmqrpc.TaskUpdate{
			Err:     fmt.Errorf("failed to unmarshal the message: %w, message_id %s, correlation_id, %s", err, taskMsg.MessageId, taskMsg.CorrelationId),
			TaskMsg: taskMsg,
		}
		return
	}

	log.Printf("Message %s corr_id %s is a PingConfiguration, destination %s", taskMsg.MessageId, taskMsg.CorrelationId, pingCfg.Destination)
	pinger := pkgsimpleping.NewSimplePinger(&pingCfg)

	log.Println("Starting to ping destination:", pingCfg.Destination, "message_id", taskMsg.MessageId, "correlation_id", taskMsg.CorrelationId)
	pingEvents := pinger.Ping(ctx)

	log.Println("Retrieving ping events about destination:", pingCfg.Destination)
	for ev := range pingEvents {
		evJson, err := json.Marshal(ev)
		if err != nil {
			log.Println("Failed to marshal ping event:", ev, "error:", err, "message_id", taskMsg.MessageId, "correlation_id", taskMsg.CorrelationId)
			continue
		}
		log.Println("Ping reply:", "type", ev.Type, "metadata", ev.Metadata, "error", ev.Error, "message_id", taskMsg.MessageId, "correlation_id", taskMsg.CorrelationId)
		updatesCh <- pkgrbmqrpc.TaskUpdate{
			Envelope: &amqp.Publishing{
				ContentType:   "application/json",
				CorrelationId: taskMsg.CorrelationId,
				Body:          evJson,
			},
			TaskMsg: taskMsg,
		}
	}
	log.Println("Ping finished, sending final task update", "message_id", taskMsg.MessageId, "correlation_id", taskMsg.CorrelationId)
	updatesCh <- pkgrbmqrpc.TaskUpdate{
		TaskMsg: taskMsg,
	}

}

// todo: implement reconnect at disconnect
func main() {
	flag.Parse()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	rbmqResponder := pkgrbmqrpc.RabbitMQResponder{
		URL: *rabbitMQBrokerURL,
	}
	rbmqResponder.SetTaskHandler(handleTask)
	log.Println("Initializing RabbitMQ responder...")
	if err := rbmqResponder.Init(); err != nil {
		log.Fatalf("Failed to initialize RabbitMQ responder: %v", err)
	}

	log.Println("Starting RabbitMQ responder...")
	rabbitMQErrCh := rbmqResponder.ServeRPC(context.Background())

	attributes := make(pkgconnreg.ConnectionAttributes)
	attributes[pkgnodereg.AttributeKeyPingCapability] = "true"

	log.Println("NodeName to advertise is:", *nodeName)
	attributes[pkgnodereg.AttributeKeyNodeName] = *nodeName

	log.Println("Waiting for queue name to be generated...")
	queueNameTimeoutCtx, canceller := context.WithTimeout(context.Background(), 10*time.Second)
	queueName, err := rbmqResponder.GetQueueNameWithContext(queueNameTimeoutCtx)
	if err != nil {
		log.Fatalf("Failed to get queue name: %v", err)
	}
	canceller()
	log.Println("QueueName to advertise will be:", queueName)

	attributes[pkgnodereg.AttributeKeyRabbitMQQueueName] = queueName
	agent := pkgnodereg.NodeRegistrationAgent{
		ServerAddress:  *addr,
		WebSocketPath:  *path,
		NodeName:       *nodeName,
		LogEchoReplies: *logEchoReplies,
	}
	agent.NodeAttributes = attributes
	log.Println("Node attributes will be announced as:", attributes)

	log.Println("Initializing node registration agent...")
	if err = agent.Init(); err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}

	log.Println("Starting node registration agent...")
	nodeRegAgentErrCh := agent.Run()

	<-sigs

	log.Println("Shutting down ping responder...")
	err = rbmqResponder.Shutdown()
	if err != nil {
		log.Fatalf("Failed to shutdown RabbitMQ responder: %v", err)
	}

	log.Println("Shutting down agent...")
	err = agent.Shutdown()
	if err != nil {
		log.Fatalf("Failed to shutdown agent: %v", err)
	}

	err = <-nodeRegAgentErrCh
	if err != nil {
		log.Printf("Agent exited with error: %v", err)
	} else {
		log.Println("Agent exited successfully")
	}

	err = <-rabbitMQErrCh
	if err != nil {
		log.Printf("RabbitMQ responder exited with error: %v", err)
	} else {
		log.Println("RabbitMQ responder exited successfully")
	}

}

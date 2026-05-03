package events

import (
	"context"
	"encoding/json"
	"time"

	"tip2/services/tasks/internal/service"

	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitPublisher struct {
	conn      *amqp.Connection
	channel   *amqp.Channel
	queueName string
	dlxName   string
	dlqName   string
}

func NewRabbitPublisher(url, queueName, dlxName, dlqName string) (*RabbitPublisher, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	if dlqName != "" {
		if dlxName != "" {
			if err := ch.ExchangeDeclare(dlxName, "direct", true, false, false, false, nil); err != nil {
				_ = ch.Close()
				_ = conn.Close()
				return nil, err
			}
		}

		if _, err := ch.QueueDeclare(dlqName, true, false, false, false, nil); err != nil {
			_ = ch.Close()
			_ = conn.Close()
			return nil, err
		}

		if dlxName != "" {
			if err := ch.QueueBind(dlqName, dlqName, dlxName, false, nil); err != nil {
				_ = ch.Close()
				_ = conn.Close()
				return nil, err
			}
		}
	}

	args := amqp.Table(nil)
	if dlqName != "" {
		args = amqp.Table{"x-dead-letter-routing-key": dlqName}
		if dlxName != "" {
			args["x-dead-letter-exchange"] = dlxName
		}
	}

	if _, err := ch.QueueDeclare(
		queueName,
		true,
		false,
		false,
		false,
		args,
	); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}

	return &RabbitPublisher{
		conn:      conn,
		channel:   ch,
		queueName: queueName,
		dlxName:   dlxName,
		dlqName:   dlqName,
	}, nil
}

func (p *RabbitPublisher) Publish(ctx context.Context, event service.TaskEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}

	publishCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	return p.channel.PublishWithContext(
		publishCtx,
		"",
		p.queueName,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now().UTC(),
			Type:         event.Type,
			Body:         body,
		},
	)
}

func (p *RabbitPublisher) PublishJob(ctx context.Context, job service.ProcessTaskJob) error {
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}

	publishCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	return p.channel.PublishWithContext(
		publishCtx,
		"",
		p.queueName,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now().UTC(),
			Type:         job.Job,
			MessageId:    job.MessageID,
			Body:         body,
		},
	)
}

func (p *RabbitPublisher) Close() error {
	if p == nil {
		return nil
	}
	if p.channel != nil {
		_ = p.channel.Close()
	}
	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
}

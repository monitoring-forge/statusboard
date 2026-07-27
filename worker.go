package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	"github.com/gammazero/workerpool"
)

var ErrorStatusCode = 255

func (o *Opt) execServiceCommand(ctx context.Context, service *Service) (int, string, error) {
	args := []string{}
	for i := 1; i < len(service.Command); i++ {
		args = append(args, service.Command[i])
	}
	output, err := exec.CommandContext(ctx, service.Command[0], args...).CombinedOutput()

	if err != nil {
		slog.Warn("run command error. service", slog.Any("service", service), slog.Any("error", err))
		if exiterr, ok := err.(*exec.ExitError); ok {
			return exiterr.ExitCode(), string(output), nil
		} else {
			return ErrorStatusCode, string(output), err
		}
	}
	return 0, string(output), nil
}

func (o *Opt) execServiceCommandWithRetry(ctx context.Context, service *Service) (int, string, error) {
	var status = ErrorStatusCode
	var output = ""
	var err error
	for retry := 0; retry < o.config.MaxCheckAttempts; retry++ {
		status, output, err = o.execServiceCommand(ctx, service)
		if status == 0 {
			break
		}
		<-time.After(o.config.RetryInterval.Duration)
	}
	return status, output, err
}

type resultMessage struct {
	status  int
	message string
	error   error
}

func (o *Opt) execServiceCommandWithTimeout(ctx context.Context, service *Service) {
	ctx, cancel := context.WithTimeout(ctx, o.config.WorkerTimeout.Duration)
	ch := make(chan resultMessage, 1)
	go func() {
		var e error
		status, message, e := o.execServiceCommandWithRetry(ctx, service)
		ch <- resultMessage{
			status:  status,
			message: message,
			error:   e,
		}
	}()
	var msg resultMessage
	select {
	case msg = <-ch:
		// nothing
	case <-ctx.Done():
		msg = resultMessage{
			status:  ErrorStatusCode,
			message: "",
			error:   fmt.Errorf("command timeout"),
		}
	}
	if msg.error != nil {
		if msg.message == "" {
			msg.message = msg.error.Error()
		}
	}
	servicelog := &ServiceLog{
		Time:         time.Now(),
		CategoryName: service.categoryName,
		Name:         service.Name,
		Command:      service.Command,
		Status:       msg.status,
		Message:      msg.message,
	}
	err := o.appendServiceLog(servicelog)
	if err != nil {
		slog.Warn("error in appendlog", slog.Any("error", err))
	}
	defer cancel()
}

func (o *Opt) execWorker(ctx context.Context) error {
	pool := workerpool.New(o.config.NumOfWorker)

	for _, category := range o.config.Categories {
		for _, s := range category.Services {
			service := s
			pool.Submit(func() {
				o.execServiceCommandWithTimeout(ctx, service)
			})
		}
	}
	pool.StopWait()
	o.renderStatusPage(ctx)

	return nil
}

func (o *Opt) startWorker(ctx context.Context) error {
	t := time.NewTicker(o.config.WorkerInterval.Duration)
	defer t.Stop()
LOOP:
	for {
		select {
		case <-t.C:
			o.execWorker(ctx)
		case <-ctx.Done():
			break LOOP
		}
	}
	return nil
}

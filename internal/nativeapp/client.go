package nativeapp

import (
	"context"
	"encoding/json"
	"local.codex.monitor/internal/monitor"
)

type response struct {
	Event string
	Data  any
}
type Client struct {
	native *monitor.Service
	events chan response
	done   chan struct{}
}

func startNativeClient(config monitor.Config) (*Client, error) {
	service, err := monitor.Start(config)
	if err != nil {
		return nil, err
	}
	c := &Client{native: service, events: make(chan response, 64), done: make(chan struct{})}
	go func() {
		defer close(c.done)
		defer close(c.events)
		for event := range service.Events {
			select {
			case c.events <- response{Event: event.Name, Data: event.Data}:
			default:
			}
		}
	}()
	return c, nil
}
func (c *Client) CallJSON(ctx context.Context, method string, args any) (json.RawMessage, error) {
	return c.native.Call(ctx, method, args)
}
func (c *Client) Call(ctx context.Context, method string, args any) (any, error) {
	return c.native.CallValue(ctx, method, args)
}

func (c *Client) Object(ctx context.Context, method string, args any) (Object, error) {
	value, err := c.Call(ctx, method, args)
	if err != nil {
		return nil, err
	}
	result := obj(value)
	normalizeLists(result)
	return result, nil
}
func (c *Client) Close() { c.native.Close(); <-c.done }

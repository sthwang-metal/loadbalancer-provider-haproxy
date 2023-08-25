package server

import (
	"context"
	"time"

	"go.infratographer.com/loadbalancer-provider-haproxy/internal/ipam"
	"go.infratographer.com/loadbalancer-provider-haproxy/internal/loadbalancer"

	"go.infratographer.com/x/events"
	"go.opentelemetry.io/otel"
)

func (s *Server) processLoadBalancerChangeCreate(ctx context.Context, lb *loadbalancer.LoadBalancer) error {
	ctx, span := otel.Tracer(instrumentationName).Start(ctx, "processLoadBalancerChangeCreate")
	defer span.End()

	// for now, limit to one IP address per loadbalancer
	if len(lb.LbData.LoadBalancer.IPAddresses) == 0 {
		if ip, err := ipam.RequestAddress(ctx, s.IPAMClient, s.Logger, s.IPBlock, lb.LoadBalancerID.String(), lb.LbData.LoadBalancer.Owner.ID); err != nil {
			return err
		} else {
			msg := events.EventMessage{
				EventType: "ip-address.assigned",
				SubjectID: lb.LoadBalancerID,
				Timestamp: time.Now().UTC(),
			}

			if _, err := s.EventsConnection.PublishEvent(ctx, "load-balancer", msg); err != nil {
				s.Logger.Debugw("failed to publish event", "error", err, "ip", ip, "loadbalancer", lb.LoadBalancerID, "block", s.IPBlock)
				return err
			}
		}
	}

	return nil
}

func (s *Server) processLoadBalancerChangeDelete(ctx context.Context, lb *loadbalancer.LoadBalancer) error {
	ctx, span := otel.GetTracerProvider().Tracer(instrumentationName).Start(ctx, "processLoadBalancerChangeCreate")
	defer span.End()

	if err := ipam.ReleaseAddress(ctx, s.IPAMClient, s.Logger, lb.LoadBalancerID.String()); err != nil {
		return err
	}

	msg := events.EventMessage{
		EventType: "ip-address.unassigned",
		SubjectID: lb.LoadBalancerID,
		Timestamp: time.Now().UTC(),
	}

	if _, err := s.EventsConnection.PublishEvent(ctx, "load-balancer", msg); err != nil {
		s.Logger.Debugw("failed to publish event", "error", err, "loadbalancer", lb.LoadBalancerID, "block", s.IPBlock)
		return err
	}

	return nil
}

package server

import (
	"errors"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"golang.org/x/exp/slices"

	"go.infratographer.com/x/events"
	"go.infratographer.com/x/gidx"

	"go.infratographer.com/loadbalancer-manager-haproxy/pkg/lbapi"

	"go.infratographer.com/loadbalancer-provider-haproxy/internal/loadbalancer"
)

var (
	defaultNakDelay = time.Second * 5
)

func (s *Server) ListenChanges(messages <-chan events.Message[events.ChangeMessage]) {
	for msg := range messages {
		s.processChange(msg)
	}
}

func (s *Server) processChange(msg events.Message[events.ChangeMessage]) {
	var lb *loadbalancer.LoadBalancer

	var err error

	m := msg.Message()

	ctx := m.GetTraceContext(s.Context)

	ctx, span := otel.Tracer(instrumentationName).Start(ctx, "processChange")
	defer span.End()

	if slices.ContainsFunc(m.AdditionalSubjectIDs, s.LocationCheck) || len(s.Locations) == 0 {
		if m.EventType != string(events.DeleteChangeType) {
			lb, err = loadbalancer.NewLoadBalancer(s.Context, s.Logger, s.APIClient, m.SubjectID, m.AdditionalSubjectIDs)
			if err != nil {
				s.Logger.Errorw("unable to initialize loadbalancer", "error", err, "messageID", msg.ID(), "message", m)

				if errors.Is(err, lbapi.ErrLBNotfound) {
					// ack and ignore
					if err = msg.Ack(); err != nil {
						s.Logger.Errorw("unable to ack message", "error", err, "messageID", msg.ID(), "message", m)
					}
				} else {
					// nack and retry
					if err = msg.Nak(defaultNakDelay); err != nil {
						s.Logger.Errorw("unable to nack message", "error", err, "messageID", msg.ID(), "message", m)
					}

					return
				}

				if lb != nil && lb.LbType != loadbalancer.TypeNoLB {
					switch {
					case m.EventType == string(events.CreateChangeType) && lb.LbType == loadbalancer.TypeLB:
						s.Logger.Debugw("requesting address for loadbalancer", "loadbalancer", lb.LoadBalancerID.String())

						if err := s.processLoadBalancerChangeCreate(ctx, lb); err != nil {
							s.Logger.Errorw("handler unable to request address for loadbalancer", "error", err, "loadbalancer", lb.LoadBalancerID.String())

							if err = msg.Nak(defaultNakDelay); err != nil {
								s.Logger.Errorw("unable to nack message", "error", err, "messageID", msg.ID(), "message", m)
							}
						}
					case m.EventType == string(events.DeleteChangeType) && lb.LbType == loadbalancer.TypeLB:
						s.Logger.Debugw("releasing address from loadbalancer", "loadbalancer", lb.LoadBalancerID.String())

						if err := s.processLoadBalancerChangeDelete(ctx, lb); err != nil {
							s.Logger.Errorw("handler unable to release address from loadbalancer", "error", err, "loadbalancer", lb.LoadBalancerID.String())

							if err = msg.Nak(defaultNakDelay); err != nil {
								s.Logger.Errorw("unable to nack message", "error", err, "messageID", msg.ID(), "message", m)
							}
						}
					default:
						s.Logger.Debugw("Ignoring event", "loadbalancer", lb.LoadBalancerID.String(), "message", m)
					}
				}
			}
			// we need to Acknowledge that we received and processed the message,
			// otherwise, it will be resent over and over again.
			if err = msg.Ack(); err != nil {
				s.Logger.Errorw("unable to ack message", "error", err, "messageID", msg.ID(), "message", m)
			}
		}
		// we need to Acknowledge that we received and processed the message,
		// otherwise, it will be resent over and over again.
		if ackErr := msg.Ack(); ackErr != nil {
			s.Logger.Errorw("error occurred while acking", "error", ackErr)
		}
	}
}

func (s *Server) LocationCheck(i gidx.PrefixedID) bool {
	for _, s := range s.Locations {
		if strings.HasSuffix(i.String(), s) {
			return true
		}
	}

	return false
}

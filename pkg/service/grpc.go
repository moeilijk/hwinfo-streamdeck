package hwsensorsservice

import (
	"context"
	"errors"
	"io"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/moeilijk/hwinfo-streamdeck/pkg/service/proto"
)

// GRPCClient is an implementation of KV that talks over RPC.
type GRPCClient struct {
	Client proto.HWServiceClient
}

// PollTime rpc call
func (c *GRPCClient) PollTime() (uint64, error) {
	resp, err := c.Client.PollTime(context.Background(), &empty.Empty{})
	if err != nil {
		return 0, err
	}
	return resp.GetPollTime(), nil
}

// Sensors implementation
func (c *GRPCClient) Sensors() ([]Sensor, error) {
	stream, err := c.Client.Sensors(context.Background(), &empty.Empty{})
	if err != nil {
		return nil, err
	}

	var sensors []Sensor
	for {
		s, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		sensors = append(sensors, &sensor{s})
	}

	return sensors, nil
}

// ReadingsForSensorID implementation
func (c *GRPCClient) ReadingsForSensorID(id string) ([]Reading, error) {
	stream, err := c.Client.ReadingsForSensorID(context.Background(), &proto.SensorIDRequest{Id: id})
	if err != nil {
		return nil, err
	}

	var readings []Reading
	for {
		r, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		readings = append(readings, &reading{r})
	}

	return readings, nil
}

// Sources implementation
func (c *GRPCClient) Sources() ([]Source, error) {
	stream, err := c.Client.Sources(context.Background(), &empty.Empty{})
	if err != nil {
		return nil, err
	}

	var sources []Source
	for {
		s, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		sources = append(sources, &source{s})
	}

	return sources, nil
}

// GRPCServer is the gRPC server that GRPCClient talks to.
type GRPCServer struct {
	// This is the real implementation
	Impl HardwareService
	proto.UnimplementedHWServiceServer
}

// PollTime gRPC wrapper
func (s *GRPCServer) PollTime(ctx context.Context, _ *empty.Empty) (*proto.PollTimeReply, error) {
	v, err := s.Impl.PollTime()
	return &proto.PollTimeReply{PollTime: v}, err
}

// Sensors gRPC wrapper
func (s *GRPCServer) Sensors(_ *empty.Empty, stream proto.HWService_SensorsServer) error {
	sensors, err := s.Impl.Sensors()
	if err != nil {
		return err
	}

	for _, sensor := range sensors {
		if err := stream.Send(&proto.Sensor{
			ID:       sensor.ID(),
			Name:     sensor.Name(),
			SourceID: sensor.SourceID(),
		}); err != nil {
			return err
		}
	}

	return nil
}

// Sources gRPC wrapper
func (s *GRPCServer) Sources(_ *empty.Empty, stream proto.HWService_SourcesServer) error {
	sources, err := s.Impl.Sources()
	if err != nil {
		return err
	}

	for _, src := range sources {
		if err := stream.Send(&proto.Source{
			ID:        src.ID(),
			Name:      src.Name(),
			PollTime:  src.PollTime(),
			Available: src.Available(),
		}); err != nil {
			return err
		}
	}

	return nil
}

// ReadingsForSensorID gRPC wrapper
func (s *GRPCServer) ReadingsForSensorID(req *proto.SensorIDRequest, stream proto.HWService_ReadingsForSensorIDServer) error {
	readings, err := s.Impl.ReadingsForSensorID(req.GetId())
	if err != nil {
		return err
	}

	for _, reading := range readings {
		if err := stream.Send(&proto.Reading{
			ID:       reading.ID(),
			TypeI:    reading.TypeI(),
			Type:     reading.Type(),
			Label:    reading.Label(),
			Unit:     reading.Unit(),
			Value:    reading.Value(),
			ValueMin: reading.ValueMin(),
			ValueMax: reading.ValueMax(),
			ValueAvg: reading.ValueAvg(),
		}); err != nil {
			return err
		}
	}

	return nil
}

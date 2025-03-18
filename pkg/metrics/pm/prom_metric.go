package pm

import (
	pb "github.com/CloudDetail/apo-receiver/internal/prometheus"
)

type PromMetric struct {
	Name   string
	Keys   []string
	Values []string
}

func newPromTimeSeries(ts int64, metric PromMetric, suffix string, value float64) *pb.TimeSeries {
	return newPromTimeSeriesWithAdditional(ts, metric, suffix, "", "", value)
}

func newPromTimeSeriesWithAdditional(ts int64, metric PromMetric, suffix string, additionalLabelName string, additionalLabelValue string, value float64) *pb.TimeSeries {
	metricLabels := make([]*pb.Label, 0)
	// Add metric-specific labels
	metricLabels = append(metricLabels, &pb.Label{
		Name:  "__name__",
		Value: metric.Name + suffix,
	})
	for i, key := range metric.Keys {
		metricLabels = append(metricLabels, &pb.Label{
			Name:  key,
			Value: metric.Values[i],
		})
	}
	if additionalLabelName != "" {
		metricLabels = append(metricLabels, &pb.Label{
			Name:  additionalLabelName,
			Value: additionalLabelValue,
		})
	}

	return &pb.TimeSeries{
		Labels: metricLabels,
		Samples: []*pb.Sample{
			{
				Value:     value,
				Timestamp: ts,
			},
		},
	}
}

func NewPromTimeSeriesWithVm(ts int64, metricName string, suffix string, labels []string, values []string, additionalLabelName string, additionalLabelValue string, value float64) *pb.TimeSeries {
	metricLabels := make([]*pb.Label, 0)
	// Add metric-specific labels
	metricLabels = append(metricLabels, &pb.Label{
		Name:  "__name__",
		Value: metricName + suffix,
	})
	for i, label := range labels {
		metricLabels = append(metricLabels, &pb.Label{
			Name:  label,
			Value: values[i],
		})
	}
	if additionalLabelName != "" {
		metricLabels = append(metricLabels, &pb.Label{
			Name:  additionalLabelName,
			Value: additionalLabelValue,
		})
	}

	return &pb.TimeSeries{
		Labels: metricLabels,
		Samples: []*pb.Sample{
			{
				Value:     value,
				Timestamp: ts,
			},
		},
	}
}

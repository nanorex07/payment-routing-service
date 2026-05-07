package metrics

import "time"

type Config struct {
	WindowSize     int
	BucketDuration time.Duration
	Threshold      float64
	MinSamples     int
	Cooldown       time.Duration
}

func DefaultConfig() Config {
	return Config{
		WindowSize:     15,
		BucketDuration: time.Minute,
		Threshold:      0.90,
		MinSamples:     10,
		Cooldown:       30 * time.Minute,
	}
}

func (c Config) WindowDuration() time.Duration {
	return time.Duration(c.WindowSize) * c.BucketDuration
}

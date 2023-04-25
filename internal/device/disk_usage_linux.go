// Copyright (c) 2023 Joshua Rich <joshua.rich@gmail.com>
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package device

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/joshuar/go-hass-agent/internal/hass"
	"github.com/lthibault/jitterbug/v2"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v3/disk"
)

type diskUsageState disk.UsageStat

// diskUsageState implements hass.SensorUpdate

func (d *diskUsageState) Name() string {
	return "Mountpoint " + d.Path + " Usage"
}

func (d *diskUsageState) ID() string {
	if d.Path == "/" {
		return "mountpoint_root"
	} else {
		return "mountpoint" + strings.ReplaceAll(d.Path, "/", "_")
	}
}

func (d *diskUsageState) Icon() string {
	return "mdi:harddisk"
}

func (d *diskUsageState) SensorType() hass.SensorType {
	return hass.TypeSensor
}

func (d *diskUsageState) DeviceClass() hass.SensorDeviceClass {
	return 0
}

func (d *diskUsageState) StateClass() hass.SensorStateClass {
	return hass.StateTotal
}

func (d *diskUsageState) State() interface{} {
	return math.Round(d.UsedPercent/0.05) * 0.05
}

func (d *diskUsageState) Units() string {
	return "%"
}

func (d *diskUsageState) Category() string {
	return ""
}

func (s *diskUsageState) Attributes() interface{} {
	return s
}

func DiskUsageUpdater(ctx context.Context, status chan interface{}) {
	sendDiskUsageStats(ctx, status)
	ticker := jitterbug.New(
		time.Minute,
		&jitterbug.Norm{Stdev: time.Second * 5},
	)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sendDiskUsageStats(ctx, status)
			}
		}
	}()

}

func sendDiskUsageStats(ctx context.Context, status chan interface{}) {
	p, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		log.Debug().Err(err).
			Msg("Could not retrieve list of physical partitions.")
		return
	}
	for _, partition := range p {
		usage, err := disk.UsageWithContext(ctx, partition.Mountpoint)
		if err != nil {
			log.Debug().Err(err).
				Msgf("Failed to get usage info for mountpount %s.", partition.Mountpoint)
			return
		}
		u := diskUsageState(*usage)
		status <- &u
	}
}

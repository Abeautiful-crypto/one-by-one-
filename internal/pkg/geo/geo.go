// Package geo：司机/订单地理位置索引（工单11 GEO 距离计算、工单17 地图订单标记）
// Redis 可用时使用 GEOADD/GEOSEARCH；降级时使用内存 Haversine 实现，接口一致。
package geo

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"ride-hailing/internal/pkg/redisx"

	"github.com/redis/go-redis/v9"
)

// Point 地理坐标
type Point struct {
	DriverID uint    `json:"driverId"`
	Lng      float64 `json:"lng"`
	Lat      float64 `json:"lat"`
}

// Neighbor GEO 检索结果（distance 单位：km）
type Neighbor struct {
	ID       uint    `json:"id"`
	Lng      float64 `json:"lng"`
	Lat      float64 `json:"lat"`
	Distance float64 `json:"distance"`
}

// Index 地理索引抽象
type Index interface {
	Add(ctx context.Context, bucket string, p Point) error
	Remove(ctx context.Context, bucket string, id uint) error
	Nearby(ctx context.Context, bucket string, lng, lat, radiusKm float64, limit int) ([]Neighbor, error)
	Available() bool
}

const geoKey = "ride:geo:" // Redis key 前缀，bucket 拼接其后

type redisGeo struct{ rdb *redis.Client }

func (g *redisGeo) key(bucket string) string { return geoKey + bucket }

func (g *redisGeo) Add(ctx context.Context, bucket string, p Point) error {
	return g.rdb.GeoAdd(ctx, g.key(bucket), &redis.GeoLocation{
		Name: itoa(p.DriverID), Longitude: p.Lng, Latitude: p.Lat,
	}).Err()
}

func (g *redisGeo) Remove(ctx context.Context, bucket string, id uint) error {
	return g.rdb.ZRem(ctx, g.key(bucket), itoa(id)).Err()
}

func (g *redisGeo) Nearby(ctx context.Context, bucket string, lng, lat, radiusKm float64, limit int) ([]Neighbor, error) {
	res, err := g.rdb.GeoSearchLocation(ctx, g.key(bucket), &redis.GeoSearchLocationQuery{
		GeoSearchQuery: redis.GeoSearchQuery{
			Longitude:  lng,
			Latitude:   lat,
			Radius:     radiusKm,
			RadiusUnit: "km",
			Sort:       "ASC",
			Count:      limit,
			CountAny:   false,
		},
		WithCoord: true, WithDist: true,
	}).Result()
	if err != nil {
		return nil, err
	}
	out := make([]Neighbor, 0, len(res))
	for _, it := range res {
		var id uint
		for _, c := range it.Name {
			if c < '0' || c > '9' {
				continue
			}
			id = id*10 + uint(c-'0')
		}
		out = append(out, Neighbor{ID: id, Lng: it.Longitude, Lat: it.Latitude, Distance: it.Dist})
	}
	return out, nil
}

func (g *redisGeo) Available() bool { return true }

func itoa(id uint) string {
	if id == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for id > 0 {
		i--
		buf[i] = byte('0' + id%10)
		id /= 10
	}
	return string(buf[i:])
}

// ---- 内存降级实现 ----

type memGeo struct {
	mu sync.RWMutex
	// bucket -> id -> Point
	data map[string]map[uint]Point
}

func (m *memGeo) Add(_ context.Context, bucket string, p Point) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		m.data = map[string]map[uint]Point{}
	}
	if m.data[bucket] == nil {
		m.data[bucket] = map[uint]Point{}
	}
	m.data[bucket][p.DriverID] = p
	return nil
}

func (m *memGeo) Remove(_ context.Context, bucket string, id uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data[bucket] != nil {
		delete(m.data[bucket], id)
	}
	return nil
}

func (m *memGeo) Nearby(_ context.Context, bucket string, lng, lat, radiusKm float64, limit int) ([]Neighbor, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []Neighbor{}
	for _, p := range m.data[bucket] {
		d := HaversineKm(lat, lng, p.Lat, p.Lng)
		if d <= radiusKm {
			out = append(out, Neighbor{ID: p.DriverID, Lng: p.Lng, Lat: p.Lat, Distance: d})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Distance < out[j].Distance })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memGeo) Available() bool { return false }

// HaversineKm 两点球面距离（km）
func HaversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * R * math.Asin(math.Sqrt(a))
}

var (
	gIdx    Index
	onceGeo sync.Once
)

// I 获取全局地理索引
func I() Index {
	onceGeo.Do(func() {
		if redisx.S().Available() {
			gIdx = &redisGeo{rdb: redisx.RDB}
		} else {
			gIdx = &memGeo{}
		}
	})
	return gIdx
}

// Bucket 业务桶常量：bucket = 前缀 + 城市/类型
func Bucket(prefix, city string) string { return prefix + ":" + city }

// 司机实时位置桶 / 待接单订单桶（工单17 地图标记数据源）
var _ = time.Second

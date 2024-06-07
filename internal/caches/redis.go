// Copyright 2024 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package caches

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/pkg"

	"github.com/go-redis/cache/v9"
	"github.com/redis/go-redis/v9"
)

type RedisServer struct {
	Host string
	Port uint16
}

const (
	ModeStandalone = "standalone"
	ModeCluster    = "cluster"
	ModeRing       = "ring"
)

var AllModes = []string{ModeStandalone, ModeCluster, ModeRing}

type RedisConfig struct {
	RedisServer               //Host and Port for a single server. A convenience equivalent to supplying Servers with a single entry
	Db          int           //Database number, defaults to 0
	KeyPrefix   string        //Prefix to keynames stored in cache
	Username    string        //Username to use to authenticate
	Password    string        //Password to use to authenticate
	Mode        string        //Controls operating mode. One of AllModes. Defaults to standalone
	Ttl         uint32        //Cache expiration in seconds. Default to 1 day
	Servers     []RedisServer //The list of servers to use.  
}

type Redis struct {
	*RedisConfig
	cache *cache.Cache
}

func ConstructRedis(config *RedisConfig, errorMessages *config.ErrorMessages) (*Redis, error) {
	var tileCache *cache.Cache

	if config.Mode == "" {
		config.Mode = ModeStandalone
	}

	if !slices.Contains(AllModes, config.Mode) {
		return nil, fmt.Errorf(errorMessages.EnumError, "cache.redis.mode", config.Mode, AllModes)
	}

	if config.Servers == nil || len(config.Servers) == 0 {
		if config.Host == "" {
			config.Host = "127.0.0.1"
		}
		if config.Port == 0 {
			config.Port = 6379
		}

		config.Servers = []RedisServer{{config.Host, config.Port}}
	} else {
		if config.Host != "" {
			return nil, fmt.Errorf(errorMessages.ParamsMutuallyExclusive, "config.redis.host", "config.redis.servers")
		}	
	}
	if config.Ttl == 0 {
		config.Ttl = 60 * 60 * 24
	}

	if config.Mode == ModeCluster {
		if config.Db != 0 {
			return nil, fmt.Errorf(errorMessages.ParamsMutuallyExclusive, "cache.redis.db", "cache.redis.cluster")
		}

		addrs := make([]string, len(config.Servers))

		for _, addr := range config.Servers {
			addrs = append(addrs, addr.Host+":"+strconv.Itoa(int(addr.Port)))
		}

		client := redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:    addrs,
			Username: config.Username,
			Password: config.Password,
		})

		//TODO: Open bug with go-redis about `rediser` type being private so the below isn't needlessly repeated
		tileCache = cache.New(&cache.Options{
			Redis: client,
		})
	} else if config.Mode == ModeRing {
		if len(config.Servers) < 2 {
			//Not the best error message but the typical user of this should be able to figure it out
			return nil, fmt.Errorf(errorMessages.InvalidParam, "length(cache.redis.servers)", len(config.Servers))
		}

		addrMap := make(map[string]string)
		for _, addr := range config.Servers {
			addrMap[addr.Host] = ":" + strconv.Itoa(int(addr.Port))
		}

		client := redis.NewRing(&redis.RingOptions{
			Addrs:    addrMap,
			Username: config.Username,
			Password: config.Password,
			DB:       config.Db,
		})

		//TODO: Open bug with go-redis about `rediser` type being private so the below isn't needlessly repeated
		tileCache = cache.New(&cache.Options{
			Redis: client,
		})
	} else {
		client := redis.NewClient(&redis.Options{
			Addr:     config.Servers[0].Host + ":" + strconv.Itoa(int(config.Servers[0].Port)),
			Username: config.Username,
			Password: config.Password,
			DB:       config.Db,
		})

		//TODO: Open bug with go-redis about `rediser` type being private so the below isn't needlessly repeated
		tileCache = cache.New(&cache.Options{
			Redis: client,
		})
	}

	r := Redis{RedisConfig: config, cache: tileCache}

	return &r, nil
}

func (c Redis) Lookup(t pkg.TileRequest) (*pkg.Image, error) {
	ctx := context.TODO()

	key := c.KeyPrefix + t.String()
	var obj pkg.Image

	err := c.cache.Get(ctx, key, &obj)

	return &obj, err
}

func (c Redis) Save(t pkg.TileRequest, img *pkg.Image) error {
	ctx := context.TODO()

	key := c.KeyPrefix + t.String()
	obj := img

	err := c.cache.Set(&cache.Item{
		Ctx:   ctx,
		Key:   key,
		Value: obj,
		TTL:   time.Duration(c.Ttl) * time.Second,
	})

	return err
}

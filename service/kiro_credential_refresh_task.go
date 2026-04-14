package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	kiroCredentialRefreshTickInterval = 10 * time.Minute
	kiroCredentialRefreshThreshold    = 1 * time.Hour
	kiroCredentialRefreshBatchSize    = 200
)

var (
	kiroCredentialRefreshOnce    sync.Once
	kiroCredentialRefreshRunning atomic.Bool
)

func StartKiroCredentialAutoRefreshTask() {
	kiroCredentialRefreshOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}

		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("kiro credential auto-refresh task started: tick=%s threshold=%s", kiroCredentialRefreshTickInterval, kiroCredentialRefreshThreshold))

			ticker := time.NewTicker(kiroCredentialRefreshTickInterval)
			defer ticker.Stop()

			runKiroCredentialAutoRefreshOnce()
			for range ticker.C {
				runKiroCredentialAutoRefreshOnce()
			}
		})
	})
}

func runKiroCredentialAutoRefreshOnce() {
	if !kiroCredentialRefreshRunning.CompareAndSwap(false, true) {
		return
	}
	defer kiroCredentialRefreshRunning.Store(false)

	ctx := context.Background()

	var refreshed int
	var scanned int

	offset := 0
	for {
		var channels []*model.Channel
		err := model.DB.
			Select("id", "name", "key", "status", "channel_info").
			Where("type = ? AND status = 1", constant.ChannelTypeKiro).
			Order("id asc").
			Limit(kiroCredentialRefreshBatchSize).
			Offset(offset).
			Find(&channels).Error
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("kiro credential auto-refresh: query channels failed: %v", err))
			return
		}
		if len(channels) == 0 {
			break
		}
		offset += kiroCredentialRefreshBatchSize

		for _, ch := range channels {
			if ch == nil {
				continue
			}
			scanned++
			if ch.ChannelInfo.IsMultiKey {
				continue
			}

			rawKey := strings.TrimSpace(ch.Key)
			if rawKey == "" {
				continue
			}

			oauthKey, err := parseKiroOAuthKey(rawKey)
			if err != nil {
				continue
			}

			refreshToken := strings.TrimSpace(oauthKey.RefreshToken)
			if refreshToken == "" {
				continue
			}

			if !isKiroTokenExpiringSoon(oauthKey, kiroCredentialRefreshThreshold) {
				continue
			}

			_, _, err = RefreshKiroChannelCredential(ctx, ch.Id, KiroCredentialRefreshOptions{ResetCaches: false})
			if err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("kiro credential auto-refresh: channel_id=%d name=%s refresh failed: %v", ch.Id, ch.Name, err))
				continue
			}

			refreshed++
			logger.LogInfo(ctx, fmt.Sprintf("kiro credential auto-refresh: channel_id=%d name=%s refreshed", ch.Id, ch.Name))
		}
	}

	if refreshed > 0 {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.LogWarn(ctx, fmt.Sprintf("kiro credential auto-refresh: InitChannelCache panic: %v", r))
				}
			}()
			model.InitChannelCache()
		}()
		ResetProxyClientCache()
	}

	if common.DebugEnabled {
		logger.LogDebug(ctx, "kiro credential auto-refresh: scanned=%d refreshed=%d", scanned, refreshed)
	}
}

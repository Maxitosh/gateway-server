package pokt_client_decorators

import (
	"errors"
	"github.com/jellydator/ttlcache/v3"
	"os-gateway/pkg/pokt/pokt_v0"
	"os-gateway/pkg/pokt/pokt_v0/models"
	"time"
)

const backoffThreshold = time.Second * 5
const sessionExpirationTtl = time.Minute * 75
const maxConcurrentDispatch = 50

var ErrRecentlyFailed = errors.New("dispatch recently failed, returning early")

type CachedClient struct {
	pokt_v0.PocketService
	lastFailure            time.Time
	concurrentDispatchPool chan struct{}
	sessionCache           *ttlcache.Cache[string, *models.GetSessionResponse]
}

func NewCachedClient(pocketService pokt_v0.PocketService) *CachedClient {
	sessionCache := ttlcache.New[string, *models.GetSessionResponse](
		ttlcache.WithTTL[string, *models.GetSessionResponse](sessionExpirationTtl),
	)
	go sessionCache.Start()

	return &CachedClient{
		PocketService:          pocketService,
		lastFailure:            time.Time{},
		sessionCache:           sessionCache,
		concurrentDispatchPool: make(chan struct{}, maxConcurrentDispatch),
	}
}

func (c *CachedClient) GetSession(req *models.GetSessionRequest) (*models.GetSessionResponse, error) {
	cacheKey := getCacheKey(req)
	cachedSession := c.sessionCache.Get(cacheKey)
	if cachedSession != nil && cachedSession.Value() != nil {
		return cachedSession.Value(), nil
	}

	// Backoff check
	if c.shouldBackoff() {
		return nil, ErrRecentlyFailed
	}

	// Limits the number of concurrent calls going out to a node
	// to prevent overloading the node during session rollover
	c.concurrentDispatchPool <- struct{}{}
	defer func() {
		<-c.concurrentDispatchPool
	}()

	// Call underlying provider
	response, err := c.PocketService.GetSession(req)
	if err != nil {
		c.lastFailure = time.Now()
		return nil, err
	}

	c.sessionCache.Set(cacheKey, response, ttlcache.DefaultTTL)
	c.lastFailure = time.Time{} // Reset last failure since it succeeded
	return response, nil
}

func (r *CachedClient) SendRelay(req *models.SendRelayRequest) (*models.SendRelayResponse, error) {

	if err := req.Validate(); err != nil {
		return nil, err
	}

	session, err := r.GetSession(&models.GetSessionRequest{AppPubKey: req.Signer.PublicKey, Chain: req.Chain})
	if err != nil {
		return nil, err
	}

	req.Session = session.Session
	return r.PocketService.SendRelay(req)
}

func (c *CachedClient) shouldBackoff() bool {
	return !c.lastFailure.IsZero() && time.Since(c.lastFailure) < backoffThreshold
}

func getCacheKey(req *models.GetSessionRequest) string {
	return req.AppPubKey + "-" + req.Chain
}
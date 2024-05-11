package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"price-feeder/oracle/provider/volume"
	"price-feeder/oracle/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"
)

var (
	_                     Provider = (*FinV2Provider)(nil)
	finV2DefaultEndpoints          = Endpoint{
		Name: ProviderFinV2,
		Urls: []string{
			"https://cosmos.directory/kujira",
			"https://lcd.kaiyo.kujira.setten.io",
			"https://lcd-kujira.mintthemoon.xyz",
		},
		PollInterval: 3 * time.Second,
		ContractAddresses: map[string]string{
			"USDCUSK": "kujira1rwx6w02alc4kaz7xpyg3rlxpjl4g63x5jq292mkxgg65zqpn5llq202vh5",
		},
	}
)

type (
	// FinV2 defines an oracle provider that uses the API of an Kujira node
	// to directly retrieve the price from the fin contract
	FinV2Provider struct {
		provider
		contracts map[string]string
		delta     map[string]int64
		volumes   volume.VolumeHistory
		height    uint64
		decimals  map[string]int64 // needs to go into endpoint
	}

	FinV2BookResponse struct {
		Data FinV2BookData `json:"data"`
	}

	FinV2BookData struct {
		Base  []FinV2Order `json:"base"`
		Quote []FinV2Order `json:"quote"`
	}

	FinV2Order struct {
		Price string `json:"quote_price"`
	}

	FinV2ConfigResponse struct {
		Data FinV2Config `json:"data"`
	}

	FinV2Config struct {
		Delta int64 `json:"decimal_delta"`
	}
)

func NewFinV2Provider(
	db *sql.DB,
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	pairs ...types.CurrencyPair,
) (*FinV2Provider, error) {
	provider := &FinV2Provider{}
	provider.Init(
		ctx,
		endpoints,
		logger,
		pairs,
		nil,
		nil,
	)

	provider.contracts = provider.endpoints.ContractAddresses

	for symbol, contract := range provider.endpoints.ContractAddresses {
		provider.contracts[contract] = symbol
	}

	availablePairs, _ := provider.GetAvailablePairs()
	provider.setPairs(pairs, availablePairs, nil)

	provider.delta = map[string]int64{}

	volumePairs := []types.CurrencyPair{}
	for _, pair := range provider.getAllPairs() {
		volumePairs = append(volumePairs, pair)
	}

	volumes, err := volume.NewVolumeHistory(logger, db, "finv2", volumePairs)
	if err != nil {
		return provider, err
	}

	provider.volumes = volumes

	provider.decimals = map[string]int64{
		"KUJI": 6,
		"USDC": 6,
		"USK":  6,
		"MNTA": 6,
		"ATOM": 6,
	}

	go startPolling(provider, provider.endpoints.PollInterval, logger)

	return provider, nil
}

func (p *FinV2Provider) Poll() error {
	missing := p.volumes.GetLatestMissing(7)
	missing = append(missing, 0)

	volumes := make([]volume.Volumes, len(missing))

	// wg := sync.WaitGroup
	// mtx := sync.Mutex

	for i, height := range missing {
		var err error
		volumes[i], err = p.getVolumes(height)
		time.Sleep(time.Millisecond * 250)
		if err != nil {
			p.logger.Err(err)
			continue
		}
	}

	err := p.volumes.AddVolumes(volumes)
	if err != nil {
		p.logger.Err(err)
	}

	timestamp := time.Now()

	p.mtx.Lock()
	defer p.mtx.Unlock()

	for symbol, pair := range p.getAllPairs() {

		contract, err := p.getContractAddress(pair)
		if err != nil {
			p.logger.Warn().
				Str("symbol", symbol).
				Msg("no contract address found")
			continue
		}

		content, err := p.wasmSmartQuery(contract, `{"book":{"limit":1}}`)
		if err != nil {
			return err
		}

		var bookResponse FinV2BookResponse
		err = json.Unmarshal(content, &bookResponse)
		if err != nil {
			return err
		}

		if len(bookResponse.Data.Base) < 1 || len(bookResponse.Data.Quote) < 1 {
			return fmt.Errorf("no order found")
		}

		base := strToDec(bookResponse.Data.Base[0].Price)
		quote := strToDec(bookResponse.Data.Quote[0].Price)

		var low, high sdk.Dec

		if base.LT(quote) {
			low = base
			high = quote
		} else {
			low = quote
			high = base
		}

		if high.GT(low.Mul(floatToDec(1.1))) {
			spread := high.Sub(low).Quo(low)
			p.logger.Error().
				Str("spread", spread.String()).
				Str("symbol", symbol).
				Msg("spread too large")
			continue
		}

		delta, err := p.getDecimalDelta(contract)
		if err != nil {
			continue
		}

		price := base.Add(quote).QuoInt64(2)
		if delta < 0 {
			price = price.Quo(uintToDec(10).Power(uint64(delta * -1)))
		} else {
			price = price.Mul(uintToDec(10).Power(uint64(delta)))
		}

		var volume sdk.Dec
		// hack to get the proper volume
		_, found := p.inverse[symbol]
		if found {
			volume = p.volumes.GetVolume(pair.Quote + pair.Base)
			if !volume.IsZero() {
				volume = volume.Quo(price)
			}
		} else {
			volume = p.volumes.GetVolume(pair.String())
		}

		p.setTickerPrice(
			symbol,
			price,
			volume,
			timestamp,
		)
	}

	p.volumes.Debug("KUJIUSDC")

	return nil
}

func (p *FinV2Provider) getVolumes(height uint64) (volume.Volumes, error) {
	p.logger.Info().Uint64("height", height).Msg("get volumes")

	var err error
	var timestamp time.Time
	var volumes volume.Volumes

	type Denom struct {
		Symbol   string
		Decimals int64
		Amount   sdk.Dec
	}

	if height == 0 {
		height, err = p.getCosmosHeight()
		if err != nil {
			return volumes, p.error(err)
		}
	}

	if height == p.height {
		return volumes, nil
	}

	// prepare all volumes:
	// not traded pairs have zero volume for this block
	values := map[string]sdk.Dec{}

	for _, pair := range p.getAllPairs() {
		values[pair.Base+pair.Quote] = sdk.ZeroDec()
		values[pair.Quote+pair.Base] = sdk.ZeroDec()
	}

	txs, timestamp, err := p.getCosmosTxs(height)
	if err != nil {
		return volumes, p.error(err)
	}

	fmt.Println("####################")
	for _, tx := range txs {
		fmt.Println("-- tx --")
		trades := tx.GetEventsByType("wasm-trade")
		for _, event := range trades {
			fmt.Println("-- event --")
			fmt.Println(event)

			contract, found := event.Attributes["_contract_address"]
			if !found {
				continue
			}

			symbol, found := p.contracts[contract]
			if !found {
				fmt.Println(contract)
				continue
			}

			pair, err := p.getPair(symbol)
			if err != nil {
				p.logger.Warn().Err(err).Msg("")
				continue
			}

			base := Denom{
				Symbol:   pair.Base,
				Decimals: p.decimals[pair.Base],
				Amount:   strToDec(event.Attributes["base_amount"]),
			}

			quote := Denom{
				Symbol:   pair.Quote,
				Decimals: p.decimals[pair.Quote],
				Amount:   strToDec(event.Attributes["quote_amount"]),
			}

			if base.Decimals == 0 && quote.Decimals == 0 {
				p.logger.Error().
					Str("pair", pair.String()).
					Msg("no decimals found")
				continue
			}

			if base.Decimals == 0 || quote.Decimals == 0 {
				delta, err := p.getDecimalDelta(contract)
				if err != nil {
					p.logger.Err(err).Msg("")
					continue
				}

				if base.Decimals == 0 {
					base.Decimals = quote.Decimals + delta
				} else {
					quote.Decimals = base.Decimals - delta
				}
			}

			ten := uintToDec(10)

			base.Amount = base.Amount.Quo(ten.Power(uint64(base.Decimals)))
			quote.Amount = quote.Amount.Quo(ten.Power(uint64(quote.Decimals)))

			// needed to for final volumes: {KUJIUSK: 1, USKKUJI: 2}
			denoms := map[string]Denom{
				pair.Base + pair.Quote: base,
				pair.Quote + pair.Base: quote,
			}

			fmt.Println(denoms)

			for symbol, denom := range denoms {
				volume, found := values[symbol]
				if !found {
					p.logger.Error().
						Str("symbol", symbol).
						Msg("volume not set")
					continue
				}

				values[symbol] = volume.Add(denom.Amount)
			}
		}
	}

	fmt.Println("height:", height)
	for symbol, value := range values {
		if value.IsZero() {
			continue
		}
		fmt.Println(symbol, value)
	}

	volumes = volume.Volumes{
		Height: height,
		Time:   timestamp.Unix(),
		Values: values,
	}

	return volumes, nil
}

func (p *FinV2Provider) GetAvailablePairs() (map[string]struct{}, error) {
	return p.getAvailablePairsFromContracts()
}

func (p *FinV2Provider) getDecimalDelta(contract string) (int64, error) {
	delta, found := p.delta[contract]
	if found {
		return delta, nil
	}

	content, err := p.wasmSmartQuery(contract, `{"config":{}}`)
	if err != nil {
		return 0, err
	}

	var response FinV2ConfigResponse

	err = json.Unmarshal(content, &response)
	if err != nil {
		p.logger.Err(err).Msg("")
		return 0, nil
	}

	delta = response.Data.Delta

	p.delta[contract] = delta

	return delta, nil
}

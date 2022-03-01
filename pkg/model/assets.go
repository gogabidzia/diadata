package models

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/diadata-org/diadata/pkg/dia"
	"github.com/ethereum/go-ethereum/common"
	"github.com/go-redis/redis"
	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v4"
)

// GetKeyAsset returns an asset's key in the redis cache of the asset table.
// @assetID refers to the primary key asset_id in the asset table.
func (rdb *RelDB) GetKeyAsset(asset dia.Asset) (string, error) {
	ID, err := rdb.GetAssetID(asset)
	if err != nil {
		return "", err
	}
	return keyAssetCache + ID, nil
}

// -------------------------------------------------------------
// Postgres methods
// -------------------------------------------------------------

// 		-------------------------------------------------------------
// 		asset TABLE methods
// 		-------------------------------------------------------------

// SetAsset stores an asset into postgres.
func (rdb *RelDB) SetAsset(asset dia.Asset) error {
	query := fmt.Sprintf("insert into %s (symbol,name,address,decimals,blockchain) values ($1,$2,$3,$4,$5)", assetTable)
	_, err := rdb.postgresClient.Exec(context.Background(), query, asset.Symbol, asset.Name, asset.Address, strconv.Itoa(int(asset.Decimals)), asset.Blockchain)
	if err != nil {
		return err
	}
	return nil
}

// GetAssetID returns the unique identifier of @asset in postgres table asset, if the entry exists.
func (rdb *RelDB) GetAssetID(asset dia.Asset) (ID string, err error) {
	query := fmt.Sprintf("select asset_id from %s where address=$1 and blockchain=$2", assetTable)
	err = rdb.postgresClient.QueryRow(context.Background(), query, asset.Address, asset.Blockchain).Scan(&ID)
	if err != nil {
		return
	}
	return
}

var assetCache = make(map[string]dia.Asset)

// GetAsset is the standard method in order to uniquely retrieve an asset from asset table.
func (rdb *RelDB) GetAsset(address, blockchain string) (asset dia.Asset, err error) {
	assetKey := "GetAsset_" + address + "_" + blockchain
	cachedAsset, found := assetCache[assetKey]
	if found {
		asset = cachedAsset
		return
	}
	var decimals string
	query := fmt.Sprintf("select symbol,name,address,decimals,blockchain from %s where address=$1 and blockchain=$2", assetTable)
	err = rdb.postgresClient.QueryRow(context.Background(), query, address, blockchain).Scan(&asset.Symbol, &asset.Name, &asset.Address, &decimals, &asset.Blockchain)
	if err != nil {
		return
	}
	decimalsInt, err := strconv.Atoi(decimals)
	if err != nil {
		return
	}
	asset.Decimals = uint8(decimalsInt)
	assetCache[assetKey] = asset
	return
}

// GetAssetByID returns an asset by its uuid
func (rdb *RelDB) GetAssetByID(assetID string) (asset dia.Asset, err error) {
	var decimals string
	query := fmt.Sprintf("select symbol,name,address,decimals,blockchain from %s where asset_id=$1", assetTable)
	err = rdb.postgresClient.QueryRow(context.Background(), query, assetID).Scan(&asset.Symbol, &asset.Name, &asset.Address, &decimals, &asset.Blockchain)
	if err != nil {
		return
	}
	decimalsInt, err := strconv.Atoi(decimals)
	if err != nil {
		return
	}
	asset.Decimals = uint8(decimalsInt)
	return
}

// GetAllAssets returns all assets on @blockchain from asset table.
func (rdb *RelDB) GetAllAssets(blockchain string) (assets []dia.Asset, err error) {
	var rows pgx.Rows
	query := fmt.Sprintf("select symbol,name,address,decimals from %s where blockchain=$1", assetTable)
	rows, err = rdb.postgresClient.Query(context.Background(), query, blockchain)
	if err != nil {
		return
	}
	defer rows.Close()

	var decimals string
	for rows.Next() {
		var asset dia.Asset
		err := rows.Scan(&asset.Symbol, &asset.Name, &asset.Address, &decimals)
		if err != nil {
			log.Error(err)
		}
		decimalsInt, err := strconv.Atoi(decimals)
		if err != nil {
			continue
		}
		asset.Decimals = uint8(decimalsInt)
		asset.Blockchain = blockchain
		assets = append(assets, asset)
	}
	return
}

// GetAssetsBySymbolName returns a (possibly multiple) dia.Asset by its symbol and name from postgres.
// If @name is an empty string, it returns all assets with @symbol.
// If @symbol is an empty string, it returns all assets with @name.
func (rdb *RelDB) GetAssetsBySymbolName(symbol, name string) (assets []dia.Asset, err error) {
	var decimals string
	var rows pgx.Rows
	if name == "" {
		query := fmt.Sprintf("select symbol,name,address,decimals,blockchain from %s where symbol=$1", assetTable)
		rows, err = rdb.postgresClient.Query(context.Background(), query, symbol)
	} else if symbol == "" {
		query := fmt.Sprintf("select symbol,name,address,decimals,blockchain from %s where name=$1", assetTable)
		rows, err = rdb.postgresClient.Query(context.Background(), query, name)
	} else {
		query := fmt.Sprintf("select symbol,name,address,decimals,blockchain from %s where symbol=$1 and name=$2", assetTable)
		rows, err = rdb.postgresClient.Query(context.Background(), query, symbol, name)
	}
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var decimalsInt int
		var asset dia.Asset
		err = rows.Scan(&asset.Symbol, &asset.Name, &asset.Address, &decimals, &asset.Blockchain)
		if err != nil {
			return
		}
		decimalsInt, err = strconv.Atoi(decimals)
		if err != nil {
			return
		}
		asset.Decimals = uint8(decimalsInt)
		assets = append(assets, asset)
	}
	return
}

// GetFiatAssetBySymbol returns a fiat asset by its symbol. This is possible as
// fiat currencies are uniquely defined by their symbol.
func (rdb *RelDB) GetFiatAssetBySymbol(symbol string) (asset dia.Asset, err error) {
	var decimals string
	query := fmt.Sprintf("select name,address,decimals from %s where symbol=$1 and blockchain='Fiat'", assetTable)
	err = rdb.postgresClient.QueryRow(context.Background(), query, symbol).Scan(&asset.Name, &asset.Address, &decimals)
	if err != nil {
		return
	}
	decimalsInt, err := strconv.Atoi(decimals)
	if err != nil {
		return
	}
	asset.Decimals = uint8(decimalsInt)
	asset.Symbol = symbol
	asset.Blockchain = "Fiat"
	// TO DO: Get Blockchain by name from postgres and add to asset
	return
}

// IdentifyAsset looks for all assets in postgres which match the non-null fields in @asset
// Comment 1: The only critical field is @Decimals, as this is initialized with 0, while an
// asset is allowed to have zero decimals as well (for instance sngls, trxc).
// Comment 2: Should we add a preprocessing step in which notation is corrected corresponding
// to the notation in the underlying contract on the blockchain?
// Comment 3: Can we improve this? How to treat cases like CoinBase emitting symbol name
// 'Wrapped Bitcoin' instead of the correct 'Wrapped BTC', or 'United States Dollar' instead
// of 'United States dollar'? On idea would be to add a table with alternative names for
// symbol tickers, so WBTC -> [Wrapped Bitcoin, Wrapped bitcoin, Wrapped BTC,...]
func (rdb *RelDB) IdentifyAsset(asset dia.Asset) (assets []dia.Asset, err error) {
	query := fmt.Sprintf("select symbol,name,address,decimals,blockchain from %s where ", assetTable)
	var and string
	if asset.Symbol != "" {
		query += fmt.Sprintf("symbol='%s'", asset.Symbol)
		and = " and "
	}
	if asset.Name != "" {
		query += fmt.Sprintf(and+"name='%s'", asset.Name)
		and = " and "
	}
	if asset.Address != "" {
		query += fmt.Sprintf(and+"address='%s'", common.HexToAddress(asset.Address).Hex())
		and = " and "
	}
	if asset.Decimals != 0 {
		query += fmt.Sprintf(and+"decimals='%d'", asset.Decimals)
		and = " and "
	}
	if asset.Blockchain != "" {
		query += fmt.Sprintf(and+"blockchain='%s'", asset.Blockchain)
	}
	rows, err := rdb.postgresClient.Query(context.Background(), query)
	if err != nil {
		return
	}
	defer rows.Close()

	var decimals string
	for rows.Next() {
		asset := dia.Asset{}
		err = rows.Scan(&asset.Symbol, &asset.Name, &asset.Address, &decimals, &asset.Blockchain)
		if err != nil {
			return
		}
		intDecimals, err := strconv.Atoi(decimals)
		if err != nil {
			log.Error("error parsing decimals string")
			continue
		}
		asset.Decimals = uint8(intDecimals)
		assets = append(assets, asset)
	}

	return
}

// 		-------------------------------------------------------------
// 		exchangesymbol TABLE methods
// 		-------------------------------------------------------------

// SetExchangeSymbol writes unique data into exchangesymbol table if not yet in there.
func (rdb *RelDB) SetExchangeSymbol(exchange string, symbol string) error {
	query := fmt.Sprintf("insert into %s (symbol,exchange) select $1,$2 where not exists (select 1 from exchangesymbol where symbol=$1 and exchange=$2)", exchangesymbolTable)
	_, err := rdb.postgresClient.Exec(context.Background(), query, symbol, exchange)
	if err != nil {
		return err
	}
	return nil
}

// GetAssets returns all assets which share the symbol ticker @symbol.
func (rdb *RelDB) GetAssets(symbol string) (assets []dia.Asset, err error) {
	query := fmt.Sprintf("select symbol,name,address,decimals,blockchain from %s where symbol=$1 ", assetTable)
	var rows pgx.Rows
	rows, err = rdb.postgresClient.Query(context.Background(), query, symbol)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var decimals string
		var decimalsInt int
		asset := dia.Asset{}
		err = rows.Scan(&asset.Symbol, &asset.Name, &asset.Address, &decimals, &asset.Blockchain)
		if err != nil {
			return
		}
		decimalsInt, err = strconv.Atoi(decimals)
		if err != nil {
			return
		}
		asset.Decimals = uint8(decimalsInt)
		assets = append(assets, asset)
	}
	return
}

// GetAssetExchnage returns all assets which share the symbol ticker @symbol.
func (rdb *RelDB) GetAssetExchange(symbol string) (exchnages []string, err error) {

	query := fmt.Sprintf("select exchange  FROM %s  INNER JOIN %s ON asset.asset_id = exchangesymbol.asset_id where exchangesymbol.symbol = $1 ", exchangesymbolTable, assetTable)
	var rows pgx.Rows
	rows, err = rdb.postgresClient.Query(context.Background(), query, symbol)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var exchange string

		err = rows.Scan(&exchange)
		if err != nil {
			return
		}
		exchnages = append(exchnages, exchange)
	}
	return
}

// GetUnverifiedExchangeSymbols returns all symbols from @exchange which haven't been verified yet.
func (rdb *RelDB) GetUnverifiedExchangeSymbols(exchange string) (symbols []string, err error) {
	query := fmt.Sprintf("select symbol from %s where exchange=$1 and verified=false order by symbol asc", exchangesymbolTable)
	var rows pgx.Rows
	rows, err = rdb.postgresClient.Query(context.Background(), query, exchange)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		symbol := ""
		err = rows.Scan(&symbol)
		if err != nil {
			return []string{}, err
		}
		symbols = append(symbols, symbol)
	}
	return
}

// GetExchangeSymbols returns all symbols traded on @exchange.
// If @exchange is the empty string, all symbols are returned.
// If @substring is not the empty string, all symbols that begin with @substring (case insensitive) are returned.
func (rdb *RelDB) GetExchangeSymbols(exchange string, substring string) (symbols []string, err error) {
	var query string
	var rows pgx.Rows
	if exchange != "" {
		if substring != "" {
			query = fmt.Sprintf("select symbol from %s where exchange=$1 and symbol ILIKE '%s%%'", exchangesymbolTable, substring)
			rows, err = rdb.postgresClient.Query(context.Background(), query, exchange)

		} else {
			query = fmt.Sprintf("select symbol from %s where exchange=$1", exchangesymbolTable)
			rows, err = rdb.postgresClient.Query(context.Background(), query, exchange)
		}
	} else {
		if substring != "" {
			query = fmt.Sprintf("select symbol from %s where symbol ILIKE '%s%%'", exchangesymbolTable, substring)
			log.Info("query: ", query)
			rows, err = rdb.postgresClient.Query(context.Background(), query)
		} else {
			query = fmt.Sprintf("select symbol from %s", exchangesymbolTable)
			rows, err = rdb.postgresClient.Query(context.Background(), query)
		}
	}
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		symbol := ""
		err = rows.Scan(&symbol)
		if err != nil {
			return []string{}, err
		}
		symbols = append(symbols, symbol)
	}
	return
}

// VerifyExchangeSymbol verifies @symbol on @exchange and maps it uniquely to @assetID in asset table.
// It returns true if symbol,exchange is present and succesfully updated.
func (rdb *RelDB) VerifyExchangeSymbol(exchange string, symbol string, assetID string) (bool, error) {
	query := fmt.Sprintf("update %s set verified=true,asset_id=$1 where symbol=$2 and exchange=$3", exchangesymbolTable)
	resp, err := rdb.postgresClient.Exec(context.Background(), query, assetID, symbol, exchange)
	if err != nil {
		return false, err
	}
	var success bool
	respSlice := strings.Split(string(resp), " ")
	numUpdates := respSlice[1]
	if numUpdates != "0" {
		success = true
	}
	return success, nil
}

// GetExchangeSymbolAssetID returns the ID of the unique asset associated to @symbol on @exchange
// in case the symbol is verified. An empty string if not.
func (rdb *RelDB) GetExchangeSymbolAssetID(exchange string, symbol string) (assetID string, verified bool, err error) {
	var uuid pgtype.UUID
	query := fmt.Sprintf("select asset_id, verified from %s where symbol=$1 and exchange=$2", exchangesymbolTable)
	err = rdb.postgresClient.QueryRow(context.Background(), query, symbol, exchange).Scan(&uuid, &verified)
	if err != nil {
		return
	}
	val, err := uuid.Value()
	if err != nil {
		log.Error(err)
	}
	if val != nil {
		assetID = val.(string)
	}
	return
}

// 		-------------------------------------------------------------
// 		exchangepair TABLE methods
// 		-------------------------------------------------------------

// GetExchangePair returns the unique exchange pair given by @exchange and @foreignname from postgres.
// It also returns the underlying pair if existent.
func (rdb *RelDB) GetExchangePair(exchange string, foreignname string) (dia.ExchangePair, error) {
	var exchangepair dia.ExchangePair

	exchangepair.Exchange = exchange
	exchangepair.ForeignName = foreignname
	var verified bool
	var uuid_quotetoken pgtype.UUID
	var uuid_basetoken pgtype.UUID

	query := fmt.Sprintf("select symbol,verified,id_quotetoken,id_basetoken from %s where exchange=$1 and foreignname=$2", exchangepairTable)
	err := rdb.postgresClient.QueryRow(context.Background(), query, exchange, foreignname).Scan(&exchangepair.Symbol, &verified, &uuid_quotetoken, &uuid_basetoken)
	if err != nil {
		return dia.ExchangePair{}, err
	}
	exchangepair.Verified = verified

	// Decode uuids and fetch corresponding assets
	val1, err := uuid_quotetoken.Value()
	if err != nil {
		log.Error(err)
	}
	if val1 != nil {
		var quotetoken dia.Asset
		quotetoken, err = rdb.GetAssetByID(val1.(string))
		if err != nil {
			return dia.ExchangePair{}, err
		}
		exchangepair.UnderlyingPair.QuoteToken = quotetoken
	}

	val2, err := uuid_basetoken.Value()
	if err != nil {
		log.Error(err)
	}
	if val2 != nil {
		basetoken, err := rdb.GetAssetByID(val2.(string))
		if err != nil {
			return dia.ExchangePair{}, err
		}
		exchangepair.UnderlyingPair.BaseToken = basetoken
	}

	return exchangepair, nil
}

// GetExchangePairSymbols returns all foreign names on @exchange from exchangepair table.
func (rdb *RelDB) GetExchangePairSymbols(exchange string) (pairs []dia.ExchangePair, err error) {
	query := fmt.Sprintf("select symbol,foreignname from %s where exchange=$1", exchangepairTable)
	var rows pgx.Rows
	rows, err = rdb.postgresClient.Query(context.Background(), query, exchange)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		pair := dia.ExchangePair{Exchange: exchange}
		err = rows.Scan(&pair.Symbol, &pair.ForeignName)
		if err != nil {
			return
		}
		pairs = append(pairs, pair)
	}
	return
}

// SetExchangePair adds @pair to exchangepair table.
// If cache==true, it is also cached into redis
func (rdb *RelDB) SetExchangePair(exchange string, pair dia.ExchangePair, cache bool) error {
	var query string
	query = fmt.Sprintf("insert into %s (symbol,foreignname,exchange) select $1,$2,$3 where not exists (select 1 from %s where symbol=$1 and foreignname=$2 and exchange=$3)", exchangepairTable, exchangepairTable)
	_, err := rdb.postgresClient.Exec(context.Background(), query, pair.Symbol, pair.ForeignName, exchange)
	if err != nil {
		return err
	}
	basetokenID, err := rdb.GetAssetID(pair.UnderlyingPair.BaseToken)
	if err != nil {
		log.Error(err)
	}
	quotetokenID, err := rdb.GetAssetID(pair.UnderlyingPair.QuoteToken)
	if err != nil {
		log.Error(err)
	}
	if basetokenID != "" {
		query = fmt.Sprintf("update %s set id_basetoken='%s' where foreignname='%s' and exchange='%s'", exchangepairTable, basetokenID, pair.ForeignName, exchange)
		_, err = rdb.postgresClient.Exec(context.Background(), query)
		if err != nil {
			return err
		}
	}
	if quotetokenID != "" {
		query = fmt.Sprintf("update %s set id_quotetoken='%s' where foreignname='%s' and exchange='%s'", exchangepairTable, quotetokenID, pair.ForeignName, exchange)
		_, err = rdb.postgresClient.Exec(context.Background(), query)
		if err != nil {
			return err
		}
	}
	query = fmt.Sprintf("update %s set verified='%v' where foreignname='%s' and exchange='%s'", exchangepairTable, pair.Verified, pair.ForeignName, exchange)
	_, err = rdb.postgresClient.Exec(context.Background(), query)
	if err != nil {
		return err
	}
	if cache {
		err = rdb.SetExchangePairCache(exchange, pair)
		if err != nil {
			log.Errorf("setting pair %s to redis for exchange %s: %v", pair.ForeignName, exchange, err)
		}
	}
	return nil
}

// -------------------------------------------------------------
// Blockchain methods
// -------------------------------------------------------------

func (rdb *RelDB) SetBlockchain(blockchain dia.BlockChain) (err error) {
	log.Info("address: ", blockchain.NativeToken.Address)
	fields := fmt.Sprintf("INSERT INTO %s (name,genesisdate,nativetoken_id,verificationmechanism,chain_id) VALUES ", blockchainTable)
	values := "($1,$2,(SELECT asset_id FROM asset WHERE address=$3 AND blockchain=$1),$4,NULLIF($5,''))"
	conflict := " ON CONFLICT (name) DO UPDATE SET genesisdate=$2,verificationmechanism=$4,chain_id=NULLIF($5,''),nativetoken_id=(SELECT asset_id FROM asset WHERE address=$3 AND blockchain=$1) "

	query := fields + values + conflict
	_, err = rdb.postgresClient.Exec(context.Background(), query,
		blockchain.Name,
		blockchain.GenesisDate,
		blockchain.NativeToken.Address,
		blockchain.VerificationMechanism,
		blockchain.ChainID,
	)
	if err != nil {
		return err
	}
	return nil
}

func (rdb *RelDB) GetBlockchain(name string) (blockchain dia.BlockChain, err error) {
	query := fmt.Sprintf("SELECT genesisdate,verificationmechanism,chain_id,address,symbol FROM %s INNER JOIN %s ON %s.nativetoken_id=%s.asset_id where %s.name=$1", blockchainTable, assetTable, blockchainTable, assetTable, blockchainTable)
	err = rdb.postgresClient.QueryRow(context.Background(), query, name).Scan(
		&blockchain.GenesisDate,
		&blockchain.VerificationMechanism,
		&blockchain.ChainID,
		&blockchain.NativeToken.Address,
		&blockchain.NativeToken.Symbol,
	)
	if err != nil {
		return
	}
	blockchain.Name = name
	return
}

// GetAllBlockchains returns all blockchain names existent in the asset table.
func (rdb *RelDB) GetAllBlockchains() ([]string, error) {
	var blockchains []string
	query := fmt.Sprintf("select distinct blockchain from %s order by blockchain asc", assetTable)
	rows, err := rdb.postgresClient.Query(context.Background(), query)
	if err != nil {
		return []string{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var blockchain string
		err := rows.Scan(&blockchain)
		if err != nil {
			return []string{}, err
		}
		blockchains = append(blockchains, blockchain)
	}

	return blockchains, nil
}

// -------------------------------------------------------------
// General methods
// -------------------------------------------------------------

// GetPage returns assets per page number. @hasNext is true iff there is a non-empty next page.
func (rdb *RelDB) GetPage(pageNumber uint32) (assets []dia.Asset, hasNextPage bool, err error) {

	pagesize := rdb.pagesize
	skip := pagesize * pageNumber
	rows, err := rdb.postgresClient.Query(context.Background(), "select symbol,name,address,decimals,blockchain from asset LIMIT $1 OFFSET $2 ", pagesize, skip)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		fmt.Println("---")
		var asset dia.Asset
		err = rows.Scan(&asset.Symbol, &asset.Name, &asset.Address, &asset.Decimals, &asset.Blockchain)
		if err != nil {
			return
		}
		assets = append(assets, asset)
	}
	// Last page (or empty page)
	if len(rows.RawValues()) < int(pagesize) {
		hasNextPage = false
		return
	}
	// No next page
	nextPageRows, err := rdb.postgresClient.Query(context.Background(), "select symbol,name,address,decimals,blockchain from asset LIMIT $1 OFFSET $2 ", pagesize, skip+1)
	if len(nextPageRows.RawValues()) == 0 {
		hasNextPage = false
		return
	}
	defer nextPageRows.Close()
	hasNextPage = true
	return
}

// Count returns the number of assets stored in postgres
func (rdb *RelDB) Count() (count uint32, err error) {
	err = rdb.postgresClient.QueryRow(context.Background(), "select count(*) from asset").Scan(&count)
	if err != nil {
		return
	}
	return
}

// -------------------------------------------------------------
// Caching layer
// -------------------------------------------------------------

// SetAssetCache stores @asset in redis, using its primary key in postgres as key.
// As a consequence, @asset is only cached iff it exists in postgres.
func (rdb *RelDB) SetAssetCache(asset dia.Asset) error {
	key, err := rdb.GetKeyAsset(asset)
	fmt.Printf("cache asset %s with key %s\n ", asset.Symbol, key)
	if err != nil {
		return err
	}
	return rdb.redisClient.Set(key, &asset, 0).Err()
}

// GetAssetCache returns an asset by its asset_id as defined in asset table in postgres
func (rdb *RelDB) GetAssetCache(assetID string) (dia.Asset, error) {
	asset := dia.Asset{}
	err := rdb.redisClient.Get(keyAssetCache + assetID).Scan(&asset)
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			log.Errorf("Error: %v on GetAssetCache with postgres asset_id %s\n", err, assetID)
		}
		return asset, err
	}
	return asset, nil
}

// CountCache returns the number of assets in the cache
func (rdb *RelDB) CountCache() (uint32, error) {
	keysPattern := keyAssetCache + "*"
	allAssets := rdb.redisClient.Keys(keysPattern).Val()
	return uint32(len(allAssets)), nil
}

// -------------- Caching exchange pairs -------------------

// SetExchangePairCache stores @pairs in redis
func (rdb *RelDB) SetExchangePairCache(exchange string, pair dia.ExchangePair) error {
	key := keyExchangePairCache + exchange + "_" + pair.ForeignName
	return rdb.redisClient.Set(key, &pair, 0).Err()
}

// GetExchangePairCache returns an exchange pair by @exchange and @foreigName
func (rdb *RelDB) GetExchangePairCache(exchange string, foreignName string) (dia.ExchangePair, error) {
	exchangePair := dia.ExchangePair{}
	err := rdb.redisClient.Get(keyExchangePairCache + exchange + "_" + foreignName).Scan(&exchangePair)
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			log.Errorf("GetExchangePairCache on %s with foreign name %s: %v\n", exchange, foreignName, err)
		}
		return exchangePair, err
	}
	return exchangePair, nil
}

func (rdb *RelDB) SetAssetVolume24H(asset dia.Asset, volume float64) error {

	initialStr := fmt.Sprintf("insert into %s (asset_id,volume) values ", assetVolumeTable)
	substring := fmt.Sprintf("((select asset_id from asset where address='%s' and blockchain='%s'),%f)", asset.Address, asset.Blockchain, volume)
	conflict := " ON CONFLICT (asset_id) do UPDATE SET volume = EXCLUDED.volume "

	query := initialStr + substring + conflict
	_, err := rdb.postgresClient.Exec(context.Background(), query)
	if err != nil {
		return err
	}
	return nil
}

func (rdb *RelDB) GetAssetVolume24H(asset dia.Asset) (volume float64, err error) {
	query := fmt.Sprintf("SELECT volume FROM %s INNER JOIN %s ON assetvolume.asset_id = asset.asset_id WHERE address=$1 AND blockchain=$2", assetVolumeTable, assetTable)
	err = rdb.postgresClient.QueryRow(context.Background(), query, asset.Address, asset.Blockchain).Scan(&volume)
	return
}

func (rdb *RelDB) GetTopAssetByVolume(symbol string) (assets []dia.Asset, err error) {
	query := fmt.Sprintf("select symbol,name,address,decimals,blockchain FROM %s INNER JOIN %s ON asset.asset_id = assetvolume.asset_id where symbol=$1 order by volume DESC", assetTable, assetVolumeTable)
	var rows pgx.Rows
	rows, err = rdb.postgresClient.Query(context.Background(), query, symbol)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var decimals string
		var decimalsInt int
		asset := dia.Asset{}
		err = rows.Scan(&asset.Symbol, &asset.Name, &asset.Address, &decimals, &asset.Blockchain)
		if err != nil {
			return
		}
		decimalsInt, err = strconv.Atoi(decimals)
		if err != nil {
			return
		}
		asset.Decimals = uint8(decimalsInt)
		assets = append(assets, asset)
	}
	return
}

func (rdb *RelDB) GetByLimit(limit, skip uint32) (assets []dia.Asset, assetIds []string, err error) {

	rows, err := rdb.postgresClient.Query(context.Background(), "select asset_id,symbol,name,address,decimals,blockchain from asset LIMIT $1 OFFSET $2 ", limit, skip)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {

		var decimals string
		var decimalsInt int
		var assetID string
		var asset dia.Asset
		err = rows.Scan(&assetID, &asset.Symbol, &asset.Name, &asset.Address, &decimals, &asset.Blockchain)
		if err != nil {
			return
		}
		decimalsInt, err = strconv.Atoi(decimals)
		if err != nil {
			return
		}
		asset.Decimals = uint8(decimalsInt)

		assets = append(assets, asset)
		assetIds = append(assetIds, assetID)
	}

	return
}

func (rdb *RelDB) GetActiveAssetCount() (count int, err error) {
	query := fmt.Sprintf("select count(*) FROM %s INNER JOIN %s ON asset.asset_id = exchangesymbol.asset_id  ", assetTable, exchangesymbolTable)
	rows := rdb.postgresClient.QueryRow(context.Background(), query)
	err = rows.Scan(&count)
	return
}

func (rdb *RelDB) GetActiveAsset(limit, skip int) (assets []dia.Asset, assetIds []string, err error) {
	query := fmt.Sprintf("select asset.asset_id,asset.symbol,name,address,decimals,blockchain FROM %s INNER JOIN %s ON asset.asset_id = exchangesymbol.asset_id order by exchangesymbol.asset_id desc Limit $1 offset $2  ", assetTable, exchangesymbolTable)
	var rows pgx.Rows
	log.Errorln("query", query)
	rows, err = rdb.postgresClient.Query(context.Background(), query, limit, skip)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var decimals string
		var decimalsInt int
		var assetID string

		asset := dia.Asset{}
		err = rows.Scan(&assetID, &asset.Symbol, &asset.Name, &asset.Address, &decimals, &asset.Blockchain)
		if err != nil {
			return
		}
		decimalsInt, err = strconv.Atoi(decimals)
		if err != nil {
			return
		}
		asset.Decimals = uint8(decimalsInt)
		assets = append(assets, asset)
		assetIds = append(assetIds, assetID)

	}
	return
}

// GetAssetsWithVOL returns the first @numAssets assets with entry in the assetvolume table, sorted by volume in descending order.
// If @numAssets==0, all assets are returned.
// If @substring is not the empty string, results are filtered by the first letters being @substring.
func (rdb *RelDB) GetAssetsWithVOL(numAssets int64, substring string) (volumeSortedAssets []dia.Asset, err error) {
	var queryString string
	var query string
	var rows pgx.Rows
	if numAssets == 0 {
		if substring == "" {
			queryString = "SELECT symbol,name,address,decimals,blockchain FROM %s INNER JOIN %s ON (asset.asset_id = assetvolume.asset_id) ORDER BY assetvolume.volume DESC"
			query = fmt.Sprintf(queryString, assetTable, assetVolumeTable)
		} else {
			queryString = "SELECT symbol,name,address,decimals,blockchain FROM %s INNER JOIN %s ON (asset.asset_id = assetvolume.asset_id) where symbol ILIKE '%s%%' ORDER BY assetvolume.volume DESC"
			query = fmt.Sprintf(queryString, assetTable, assetVolumeTable, substring)
		}
	} else {
		if substring == "" {
			queryString = "SELECT symbol,name,address,decimals,blockchain FROM %s INNER JOIN %s ON (asset.asset_id = assetvolume.asset_id) ORDER BY assetvolume.volume DESC limit %d"
			query = fmt.Sprintf(queryString, assetTable, assetVolumeTable, numAssets)
		} else {
			queryString = "SELECT symbol,name,address,decimals,blockchain FROM %s INNER JOIN %s ON (asset.asset_id = assetvolume.asset_id) where symbol ILIKE '%s%%' ORDER BY assetvolume.volume DESC limit %d"
			query = fmt.Sprintf(queryString, assetTable, assetVolumeTable, substring, numAssets)
		}
	}

	rows, err = rdb.postgresClient.Query(context.Background(), query)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var decimals string
		var decimalsInt int
		asset := dia.Asset{}
		err = rows.Scan(&asset.Symbol, &asset.Name, &asset.Address, &decimals, &asset.Blockchain)
		if err != nil {
			return
		}
		decimalsInt, err = strconv.Atoi(decimals)
		if err != nil {
			return
		}
		asset.Decimals = uint8(decimalsInt)
		volumeSortedAssets = append(volumeSortedAssets, asset)
	}
	return
}

// GetAssetsWithVOLInflux returns all assets that have an entry in Influx's volumes table and hence have been traded since @timeInit.
func (datastore *DB) GetAssetsWithVOLInflux(timeInit time.Time) ([]dia.Asset, error) {
	var quotedAssets []dia.Asset
	q := fmt.Sprintf("SELECT address,blockchain,value FROM %s WHERE filter='VOL120' AND exchange='' AND time>%d and time<now()", influxDbFiltersTable, timeInit.UnixNano())
	res, err := queryInfluxDB(datastore.influxClient, q)
	if err != nil {
		return quotedAssets, err
	}

	// Filter and store all unique assets from the filters table.
	uniqueMap := make(map[dia.Asset]struct{})
	if len(res) > 0 && len(res[0].Series) > 0 {
		if len(res[0].Series[0].Values) > 0 {
			var asset dia.Asset
			for _, val := range res[0].Series[0].Values {
				if val[1] == nil || val[2] == nil {
					continue
				}
				asset.Address = val[1].(string)
				asset.Blockchain = val[2].(string)
				if _, ok := uniqueMap[asset]; !ok {
					quotedAssets = append(quotedAssets, asset)
					uniqueMap[asset] = struct{}{}
				}
			}
		} else {
			return quotedAssets, errors.New("no recent assets with volume in influx")
		}
	} else {
		return quotedAssets, errors.New("no recent asset with volume in influx")
	}
	return quotedAssets, nil
}
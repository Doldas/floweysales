package storage

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
)

// PaperCashRisk is expressed in the account's single quote currency.
// This feature deliberately does not convert multiple quote currencies.
type PaperCashRisk struct {
	StartingCash    float64
	CommissionPct   float64
	CommissionFixed float64
	SectorLimitPct float64
	PortfolioEquity float64
	HeldSectorExposure float64
	SymbolSectors map[string]string
}

func (r PaperCashRisk) fee(amount float64) float64 {
	return math.Round((math.Abs(amount)*r.CommissionPct/100+r.CommissionFixed)*100) / 100
}

func (r PaperCashRisk) valid() bool {
	return !math.IsNaN(r.StartingCash) && !math.IsInf(r.StartingCash, 0) && r.StartingCash >= 0 &&
		!math.IsNaN(r.CommissionPct) && !math.IsInf(r.CommissionPct, 0) && r.CommissionPct >= 0 &&
		!math.IsNaN(r.CommissionFixed) && !math.IsInf(r.CommissionFixed, 0) && r.CommissionFixed >= 0
}

// lockPaperCash validates the account-wide available cash against *committed*
// trades and all OPEN BUY orders, while holding MySQL's singleton account lock.
// This must be called in the SAME transaction as the new order insert.
func lockPaperCash(tx *sql.Tx, order Order, risk PaperCashRisk) error {
	if !risk.valid() {
		return fmt.Errorf("invalid paper cash risk configuration")
	}
	var lockID int
	if err := tx.QueryRow("SELECT id FROM paper_account_lock WHERE id=1 FOR UPDATE").Scan(&lockID); err != nil {
		return fmt.Errorf("paper cash lock unavailable: %w", err)
	}
	if order.Side=="BUY" && risk.SectorLimitPct>0 {
		if math.IsNaN(risk.SectorLimitPct)||math.IsInf(risk.SectorLimitPct,0)||risk.SectorLimitPct>100||
			risk.PortfolioEquity<=0||math.IsNaN(risk.PortfolioEquity)||math.IsInf(risk.PortfolioEquity,0)||
			risk.HeldSectorExposure<0||math.IsNaN(risk.HeldSectorExposure)||math.IsInf(risk.HeldSectorExposure,0) {
			return fmt.Errorf("invalid sector concentration risk input")
		}
		target:=strings.TrimSpace(risk.SymbolSectors[order.Symbol])
		if target=="" { return fmt.Errorf("missing target symbol sector classification") }
		rows,err:=tx.Query("SELECT symbol,quantity,price,limit_price,stop_price FROM orders WHERE status='OPEN' AND side='BUY'")
		if err!=nil{return err}
		exposure:=risk.HeldSectorExposure
		for rows.Next(){
			var symbol string
			var qty,price,limit,stop float64
			if err:=rows.Scan(&symbol,&qty,&price,&limit,&stop);err!=nil {rows.Close();return err}
			sector:=strings.TrimSpace(risk.SymbolSectors[symbol])
			if sector=="" {rows.Close();return fmt.Errorf("missing sector for existing open order %s",symbol)}
			if strings.EqualFold(sector,target){
				worst:=math.Max(price,math.Max(limit,stop))
				exposure+=qty*worst
			}
		}
		readErr:=rows.Err()
		rows.Close()
		if readErr!=nil{return readErr}
		worst:=math.Max(order.Price,math.Max(order.LimitPrice,order.StopPrice))
		if order.Status=="FILLED" && order.FillPrice>0 {worst=order.FillPrice}
		if exposure+order.Quantity*worst>risk.PortfolioEquity*risk.SectorLimitPct/100+1e-8 {
			return fmt.Errorf("sector allocation exceeds %.2f%% under locked account snapshot",risk.SectorLimitPct)
		}
	}
	if order.Side != "BUY" {
		return nil
	}
	rows, err := tx.Query(`SELECT side,status,quantity,fill_price,fee,price,limit_price,stop_price
		FROM orders WHERE status IN ('OPEN','FILLED')`)
	if err != nil {
		return err
	}
	available := risk.StartingCash
	for rows.Next() {
		var side, status string
		var qty, fill, fee, price, limit, stop float64
		if err := rows.Scan(&side, &status, &qty, &fill, &fee, &price, &limit, &stop); err != nil {
			rows.Close()
			return err
		}
		if status == "FILLED" {
			if side == "BUY" {
				available -= qty*fill + fee
			} else if side == "SELL" {
				available += qty*fill - fee
			}
		} else if side == "BUY" {
			worst := math.Max(price, math.Max(limit, stop))
			available -= qty*worst + risk.fee(qty*worst)
		}
	}
	readErr := rows.Err()
	rows.Close()
	if readErr != nil {
		return readErr
	}
	worst := math.Max(order.Price, math.Max(order.LimitPrice, order.StopPrice))
	if order.Status == "FILLED" && order.FillPrice > 0 {
		worst = order.FillPrice
	}
	cost := order.Quantity*worst + risk.fee(order.Quantity*worst)
	if math.IsNaN(cost) || math.IsInf(cost, 0) || cost <= 0 || math.IsNaN(available) || math.IsInf(available, 0) {
		return fmt.Errorf("invalid account cash calculation")
	}
	if cost > available+1e-8 {
		return fmt.Errorf("insufficient unreserved paper cash: need %.2f, available %.2f", cost, available)
	}
	return nil
}

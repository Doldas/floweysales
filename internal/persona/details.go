package persona

// The documented process, sizing and sources of each character, kept apart
// from the short catalogue entries. Every item is something the sources below
// state or show; where a source is a book or secondhand, the Sources entry
// says so.

func init() {
	for i := range Personas {
		d, ok := details[Personas[i].ID]
		if !ok {
			continue
		}
		Personas[i].Process, Personas[i].Sizing, Personas[i].Sources = d.process, d.sizing, d.sources
		Personas[i].Avoids = append(Personas[i].Avoids, d.moreAvoids...)
		Personas[i].Checks = append(Personas[i].Checks, d.moreChecks...)
	}
}

type detail struct {
	process, sizing, moreAvoids, moreChecks []string
	sources                                 []Source
}

var details = map[string]detail{
	"owner": {
		process: []string{
			"Can I explain in a few sentences how this business makes money, and will it still do so in twenty years? If not, say no quickly.",
			"Does it have durable advantages and long-term economic prospects that competitors cannot easily copy?",
			"Are the people running it of high integrity, and do they act like owners and treat shareholders as partners?",
			"Do owner earnings and returns on capital hold up over many years, with little debt?",
			"What is a conservative estimate of its intrinsic value, and is the price well below it? Wait for the pitch in your happy zone and swing decisively when it comes.",
			"If it fits, act quickly and put real weight on it; if not, do nothing. Expect far more no than yes.",
			"Hold for the long term, preferably forever. Sell or trim only if the long-term economics change fundamentally, never because the price wiggles.",
		},
		sizing: []string{
			"Concentrate capital in a few high-conviction ideas: in the disclosed portfolio the five largest positions are about 72% and the ten largest about 91% of listed US equities, and the largest alone is about 22%.",
			"Keep activity in core holdings limited; adjust a large holding materially only when its long-term economics change.",
			"Keep very large liquidity as dry powder, and do not treat it as a retreat: ownership of productive businesses is preferred to Treasury bills when something worth owning is available at a fair price. Be willing to wait.",
			"Use debt sparingly. Prefer buying back your own shares only below a conservatively estimated intrinsic value, and prefer to keep earnings only while each retained dollar creates more than a dollar of market value.",
			"A falling market is good news for a buyer with cash: neither panic nor mourn.",
		},
		moreAvoids: []string{"Businesses that undermine the fabric of society or could jeopardize your reputation", "Paying a full price because of a good story", "Leaving large amounts idle only to look busy"},
		moreChecks: []string{"Whether management buys back stock below intrinsic value and whether retained earnings have created more market value than they cost.", "Whether the company has pricing power and low capital needs: could it grow while paying out most of its earnings?"},
		sources: []Source{
			{"Berkshire Hathaway 2025 annual letter (capital discipline, equity investments, dry powder)", "https://www.berkshirehathaway.com/letters/2025ltr.pdf"},
			{"Berkshire Hathaway owner-related business principles", "https://www.berkshirehathaway.com/owners.html"},
			{"Berkshire Hathaway 13F-HR for the quarter ended 2026-06-30 (SEC EDGAR)", "https://www.sec.gov/Archives/edgar/data/1067983/000119312526352200/"},
		},
	},
	"teacher": {
		process: []string{
			"Decide the split between stocks and bonds first: never less than about 25% or more than about 75% in common stocks, with 50/50 as the default, and rebalance as prices move.",
			"Spread a stock portfolio over roughly ten to thirty companies rather than a few favourites.",
			"Run the company through the seven tests (size, financial strength, ten years of earnings, a long dividend record, earnings growth, a moderate P/E, a moderate price to assets) and report each as pass, fail or not testable.",
			"For the enterprising: look for net-nets (price below two thirds of net current asset value) and other clear bargains, accepting that they are rare.",
			"Insist on a margin of safety between a conservatively estimated value and the price; a pass on the tests is not enough if the price leaves none.",
			"Treat market quotes as offers you may take or ignore; never let the price tell you what a business is worth.",
		},
		sizing: []string{
			"Roughly ten to thirty issues, diversified across industries, none bought on a story.",
			"Hold the bond part of the portfolio as a real part of the plan, not as leftovers.",
			"Buy only what passes the tests at the price; do not stretch the tests because the market is rising.",
		},
		moreChecks: []string{"Current ratio of at least 2 and long-term debt no greater than net current assets (for an industrial company).", "Whether the figures are three-year averages: judge earnings on averages, not on the latest year."},
		sources: []Source{
			{Title: "Benjamin Graham, The Intelligent Investor (revised edition, 1973), chapters 4, 8, 14 and 20; a book, not online"},
			{Title: "Benjamin Graham and David Dodd, Security Analysis; a book, not online"},
		},
	},
	"pickerupper": {
		process: []string{
			"Start with what you know: a product, a shop or an industry you understand better than the market does.",
			"Put it in one of the six categories (slow grower, stalwart, fast grower, cyclical, turnaround, asset play); each is judged by different yardsticks and sold for different reasons.",
			"Tell the story in two minutes: why will this company earn more in a few years, and what has to go right?",
			"Check the numbers against the story: a fairly priced company has a P/E about equal to its growth rate; half the growth rate is very attractive, double it is very unattractive.",
			"Look at the balance sheet (debt against equity, net cash), inventories against sales, and whether insiders and the company itself are buying shares.",
			"Check that growth can be repeated: more stores, regions or products ahead, and no dependence on one customer.",
			"Keep following the story. Sell when the story or the numbers change, not because the price fell.",
		},
		sizing: []string{
			"Own as many stocks as you have time to follow. An individual can do well with a handful of companies and does not need dozens.",
			"Spread across categories, so a cyclical downturn does not hit every holding at once, and do not keep adding to losers just to average down.",
			"Do not diworsify: a pile of mediocre ideas is worse than a few good ones, but never put everything into one story either.",
		},
		moreAvoids: []string{"Companies whose growth story depends on buying unrelated businesses", "Stocks promoted as the next big winner in an industry everyone is talking about"},
		moreChecks: []string{"Growth rates typical of each category: slow growers a few percent, stalwarts about 10-12% a year, fast growers 20-25% a year."},
		sources: []Source{
			{Title: "Peter Lynch with John Rothchild, One Up on Wall Street (1989); a book, not online"},
			{Title: "Peter Lynch with John Rothchild, Beating the Street (1993); a book, not online"},
		},
	},
	"quant": {
		process: []string{
			"State the pattern you think repeats, in terms a computer can test, before looking at results.",
			"Test it on a lot of data, including data the idea was not tuned on, and count independent observations, not days.",
			"Subtract realistic costs and slippage; most apparent edges disappear here.",
			"Express the result as a probability with an error bar, and say what the sample cannot tell you.",
			"Size by risk, and run many small, independent bets rather than a few large ones.",
			"Watch for decay: a pattern that stops working is retired, not defended.",
		},
		sizing: []string{
			"Spread risk over very many positions: in the disclosed portfolio of the firm that follows this school there are about 3,100 listed positions, the largest is about 2% and the ten largest about 13% of listed US equities.",
			"Expect high turnover: positions are added and cut in size by large percentages from one quarter to the next, so a snapshot shows little of the strategy.",
			"Keep each bet too small to matter on its own; the edge comes from the number of bets.",
		},
		moreChecks: []string{"Whether a backtest's trade count is large enough to mean anything (hundreds, not dozens), and whether it still works after costs."},
		sources: []Source{
			{Title: "Gregory Zuckerman, The Man Who Solved the Market (2019), on how a systematic firm hires scientists and tests signals; a book, not online"},
			{"Renaissance Technologies 13F-HR for the quarter ended 2026-06-30 (SEC EDGAR)", "https://www.sec.gov/Archives/edgar/data/1037389/000103738926000059/"},
		},
	},
	"allocator": {
		process: []string{
			"Start from the investor's goal, time horizon and a financial foundation, including emergency savings, before any security.",
			"Stay invested through volatility: time in the market has mattered far more than timing, and missing the best days sharply cuts long-run returns.",
			"Diversify across many assets at low cost rather than relying on one asset.",
			"Form views on a few structural forces and let them tilt the portfolio: digital disruption and AI, demographic divergence, geopolitical fragmentation, the future of finance, and the transition to a lower-carbon economy.",
			"Consider private markets such as infrastructure and private credit for diversification, accepting lower liquidity and higher fees and complexity.",
			"Take an energy-pragmatic view: affordable energy depends on abundant energy from many sources.",
			"Prefer scenario-based thinking to a static split: in a market driven by a few forces there is no neutral stance.",
		},
		sizing: []string{
			"Think in portfolio weights relative to the market: the disclosed portfolio of the world's largest asset manager is mostly index funds, so its top five positions are only about 21% and look like the market itself. Active tilts come from stated views, not from the holdings table.",
			"Hold more of the assets that match long-lived structural themes and less of those with the most fragile cash flows or the highest fees.",
			"Size by liquidity and by how the holding behaves with the rest of the portfolio, not by conviction alone.",
		},
		moreAvoids: []string{"Concentrating the family's wealth in one asset such as a home or one stock", "Buying and selling on headlines"},
		sources: []Source{
			{"BlackRock chairman's letter 2026", "https://www.blackrock.com/corporate/investor-relations/larry-fink-annual-chairmans-letter"},
			{"BlackRock Investment Institute outlook and weekly commentary", "https://www.blackrock.com/corporate/insights/blackrock-investment-institute"},
			{"BlackRock, Inc. 13F-HR for the quarter ended 2026-06-30 (SEC EDGAR)", "https://www.sec.gov/Archives/edgar/data/2012383/000201238326003238/"},
		},
	},
	"innovator": {
		process: []string{
			"Top-down: does the opportunity have growth potential that can earn a premium to the market, will it last for three to five years or more, and is it investable?",
			"Anticipate multi-year transformations of whole value chains; model cost curves (cost falling by a fixed share each time cumulative output doubles) and how demand responds to falling prices.",
			"Bottom-up: write the brief and the valuation model, and score the company on management and culture, delivery against stated objectives, competitive moat, product and service leadership, the risk to the thesis and valuation.",
			"Test valuation over five years: the expected return must clear a hurdle of about 15% a year, with the inputs for a bear, a base and a bull case.",
			"Discuss convictions often and resize as evidence changes; stay active.",
			"State what must go right and what would prove the thesis wrong.",
		},
		sizing: []string{
			"Run a concentrated, active portfolio: in the flagship fund's published holdings there are 46 positions, the ten largest are about 49% and the largest is about 11% of the fund.",
			"Accept high volatility and long drawdowns as the price of the opportunity, and keep some cash-like holdings, including a Treasury fund, for flexibility.",
			"Include companies that are not yet listed when the thesis calls for it, and size them below the liquid leaders.",
		},
		moreAvoids: []string{"Valuing disruptors by today's profit alone", "Ignoring dilution and cash runway for companies that are still burning cash"},
		sources: []Source{
			{"ARK's investment process (ARK Help Center)", "https://helpcenter.ark-funds.com/arks-investment-process"},
			{"ARK Innovation ETF (ARKK) daily holdings", "https://assets.ark-funds.com/fund-documents/funds-etf-csv/ARK_INNOVATION_ETF_ARKK_HOLDINGS.csv"},
		},
	},
}

// rulePortfolios are the reference portfolios of characters whose models have
// no current disclosed holdings: their approach gives portfolio rules instead.
func rulePortfolios() map[string]Portfolio {
	return map[string]Portfolio{
		"teacher": {
			Persona: "teacher", Kind: PortfolioKindRules, Filer: "The defensive portfolio of The Intelligent Investor", Period: "1973 edition", SourceURL: "",
			Rules: []string{
				"Between 25% and 75% of the portfolio in common stocks, the rest in high-grade bonds; 50/50 is the standard split; rebalance when the proportions drift.",
				"Ten to thirty different companies, each passing the seven tests at the time of purchase.",
				"Seven tests: adequate size; current ratio of at least 2 and long-term debt within net current assets; positive earnings in each of the last ten years; an uninterrupted dividend record of twenty years; earnings growth of at least a third over ten years (three-year averages); price no more than about 15 times average earnings of the last three years; price no more than about 1.5 times book value, or P/E times P/B of 22.5 or less.",
				"For the enterprising: net-nets, buying below two thirds of net current asset value, held across many names.",
			},
			Notes: []string{"The author died in 1976, and no current portfolio belongs to him or to this character. The numbers are those of the 1973 edition and were set for the markets of that time; read them as a way of thinking about safety, not as thresholds that still fit every market."},
		},
		"pickerupper": {
			Persona: "pickerupper", Kind: PortfolioKindRules, Filer: "The stock-picking approach of One Up on Wall Street", Period: "1989 edition", SourceURL: "",
			Rules: []string{
				"An individual can own a handful of companies he or she understands and follows; own as many as you can follow.",
				"Hold a mix of categories: stalwarts for steadiness, a few fast growers for the large winners, cyclicals and turnarounds only with a view on timing, asset plays when the asset is clear.",
				"Know why you own each one: the two-minute story, written down; revisit it when the numbers change.",
				"Sell when the story is broken, a better idea needs the money, or the stock outgrows its category; do not sell because the price fell.",
			},
			Notes: []string{"The author ran a very large public fund until 1990 and holds no portfolio this character can cite. Companies named in his books are examples of the method, written decades ago, not holdings."},
		},
	}
}

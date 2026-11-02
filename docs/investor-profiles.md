# Investerarprofiler: analys av förebilderna bakom AI-karaktärerna

Det här dokumentet är underlaget för de sex AI-karaktärerna (se [agent-personas.md](agent-personas.md)). Karaktärerna är **påhittade** och har påhittade namn, men var och en följer en känd investerares dokumenterade arbetssätt och, där det finns, den offentligt redovisade portföljen. Här står vad som är verifierat, vad som är sekundärt, vad vi har fört in i karaktärerna och vad som inte går att återskapa.

Portföljsiffrorna är hämtade direkt från SEC EDGAR och ARK den 2 november 2026 (`scripts/refresh_reference_portfolios.py`). Allt som inte kommer från en primärkälla är markerat.

## 1. Först: vad som går att mimicka och vad som inte gör det

Av de sex förebilderna har bara tre en portfölj som idag redovisas offentligt, och ingen av dem redovisar *varför*:

| Förebild | Status | Portfölj som kan citeras | Konfidens i underlaget |
| --- | --- | --- | --- |
| Warren Buffett | Ordförande i Berkshire Hathaway; Greg Abel är VD sedan årsskiftet 2025/26 | Berkshires 13F (kvartalsslut 2026-06-30) | Hög: eget brev och ägarmanual |
| Benjamin Graham | Död 1976 | Ingen. Regler för en defensiv portfölj finns i boken | Medel: bok, ingen onlinekälla |
| Peter Lynch | Lämnade Magellan 1990 | Ingen. Exempel finns i hans böcker | Medel: bok och sekundärkällor |
| Jim Simons | Död 2024. Renaissance drivs vidare av andra | Renaissance 13F (2026-06-30), men den säger lite om strategin | Låg för metoder: hemliga; medel för filosofi (Zuckermans bok) |
| Larry Fink | VD för BlackRock | BlackRock 13F: indexlik och inte hans åsikt | Hög för åsikter (eget brev), låg för portföljen som "vy" |
| Cathie Wood | VD för ARK Invest | ARKK:s dagliga innehav (2026-11-02) | Hög: ARK publicerar process och innehav |

Konsekvenser som är byggda in i karaktärerna:

- Karaktärerna får **inte** påstå att en verklig person äger eller tycker något i dag. De får citera en portfölj som *"den redovisade portföljen hos <filer> per <datum>"*, med vikt och rang, och måste kalla det en redovisning och inte en vy.
- För Graham och Lynch finns ingen portfölj. Deras karaktärer får i stället **portföljregler** (Grahams 25–75 %-spann och 10–30 bolag; Lynchs kategorier och uppföljning) och märks som historiska.
- För Renaissance och BlackRock är 13F-tabellen nästan meningslös som fingervisning om strategin. Det står i karaktärens förbehåll, och det som faktiskt härmas är **portföljens form** (spridning respektive indexlikhet).

## 2. Källor och begränsningar för 13F

13F-HR är en kvartalsrapport över *långa positioner i börsnoterade amerikanska värdepapper* som ett förvaltarhus har investeringsbeslut över. Den:

- lämnas in upp till 45 dagar efter kvartalets slut, så den är minst 6 veckor gammal när den publiceras (siffrorna här är per 2026-06-30 och lämnades in i augusti);
- visar **inte** shorts, obligationer, kontanter, utländska innehav, rörelsedrivande dotterbolag eller derivat (optionsrader är bortfiltrerade här);
- anger CUSIP, inte ticker. Mappningen CUSIP till ticker finns i skriptet och är kontrollerad för de största positionerna; ett okänt CUSIP behåller sitt namn utan ticker;
- slår ihop flera aktieslag av samma bolag (Alphabets A- och C-aktier är en post, liksom Lennars båda slag).

Vikter nedan är **andel av den redovisade 13F-portföljen**, inte andel av förmögenhet eller förvaltat kapital.

## 3. Profiler

### 3.1 Warren Buffett, karaktären Walter Ashgrove (`owner`)

**Hur han investerar (primärkällor).** Berkshires brev för 2025, det första som Greg Abel skrev som VD, och Berkshires ägarmanual är tydliga:

- *Förstå verksamheten.* "Invest in businesses that we thoroughly understand, with durable advantages and long-term economic prospects." Partner med ledare med hög integritet som agerar som ägare. Undvik verksamheter som skadar samhällsväven eller bolagets rykte.
- *Koncentrera.* "Act quickly and concentrate our capital in a few high conviction ideas", och sedan "let compounding unfold". Brevet säger att den koncentrerade aktieportföljen fortsätter, med begränsad aktivitet i kärninnehaven, men att ett innehav kan ändras kraftigt om dess långsiktiga ekonomi förändras i grunden.
- *Tålamod.* Brevet använder Ted Williams "happy zone": bestäm vilka bollar man vill slå på, vänta, och slå beslutsamt. De säger snabbt nej till det som inte passar, och att det finns många fler nej än ja.
- *Kontanter som torrt krut.* Kontanter och amerikanska statsskuldsväxlar översteg 370 miljarder dollar vid årsskiftet. Det är inget tecken på reträtt; "we will always aim for ownership of productive businesses over U.S. Treasuries".
- *Egna aktier och utdelning.* Återköp görs när aktien handlas under en konservativt bedömd inneboende värde. Ingen utdelning så länge varje behållen dollar kan skapa mer än en dollar i marknadsvärde.
- *Skuld sparsamt.* Balansräkningen är en strategisk tillgång.
- *Ägarmanualen* (äldre men fortfarande gällande): målet är tillväxt i inneboende värde per aktie; föredrar att äga hela företag, annars delar; en fallande börs är goda nyheter för en köpare; "neither panic nor mourn".

**Portföljen (SEC 13F-HR, 2026-06-30, inlämnad 2026-08-14, accession 0001193125-26-352200).** 27 positioner, **299,3 miljarder dollar**.

| Mått | Värde |
| --- | --- |
| Största position (Apple) | 22,0 % |
| Fem största / tio största | 71,9 % / 91,0 % |
| Herfindahl-index | 1 229 (mycket koncentrerad) |
| Finans (American Express, Bank of America, Chubb, Moody's, m.fl.) | cirka 34,6 % |
| Teknik och kommunikation (Apple, Alphabet, Verisign) | cirka 35,4 % |
| Dagligvaror (Coca-Cola, Kraft Heinz, Kroger) | cirka 14,2 % |
| Energi (Chevron, Occidental) | cirka 9,0 % |

Största innehav: Apple 22,0 %, American Express 17,1 %, Alphabet 12,6 %, Coca-Cola 10,9 %, Bank of America 9,2 %, Chevron 4,7 %, Occidental 4,3 %, Chubb 3,9 %, Moody's 3,7 %, Kraft Heinz 2,6 %.

Förändring i antal aktier mot kvartalet innan (Q2 mot Q1 2026): Alphabet **+83 %** (A- och C-aktier tillsammans), Delta Air Lines +44 %, Lennar +30 %, Macy's +142 %, Kroger −22 %, Capital One −58 %, Nucor −52 %, Bank of America −6 %, Davita −4 %; helt sålt Constellation Brands; ny post D.R. Horton. Apple, American Express och Coca-Cola är oförändrade.

**Det 13F inte visar.** Rörelsedrivande bolag (BNSF, GEICO, med flera), de japanska handelshusen (Mitsubishi, ITOCHU, Mitsui, Marubeni, Sumitomo; cirka 35 miljarder dollar enligt brevet), Occidental och Kraft Heinz som kapitalandelsinnehav, samt kontanterna (cirka 360 miljarder dollar i andra kvartalet enligt en sekundärkälla). Det är därför ett 13F-flöde som "köp Apple" aldrig är samma sak som Berkshires strategi.

**Vad karaktären har fått.** Beslutsordning i sju steg (kompetenscirkel, vallgrav, ägarmentalitet, avkastning på kapital, konservativt värde, snabbt ja eller nej, håll i decennier); portföljregler (koncentration med siffrorna ovan, begränsad aktivitet, torrt krut utan tvång, återköp bara under värde); kontroller av återköp och retained earnings-testet; en referensportfölj med datum som den kan jämföra ett bolag mot; strategin `owner_compounder` (rankning) och `persona_owner_dip` (regler).

**Vad vi inte kan återskapa.** Ledningens integritet, vallgravens bestånd och priset man betalar vid en panik går inte att mäta i faktorer. Karaktären säger det.

### 3.2 Benjamin Graham, karaktären Bernhard Holm (`teacher`)

**Källa.** *The Intelligent Investor* (reviderad upplaga 1973), kapitel 4, 8, 14 och 20, samt *Security Analysis*. Böckerna finns inte online; innehållet är hämtat från bokens kända struktur och inte kontrollerat mot en onlinekälla i den här körningen.

**Det defensiva upplägget.**

1. *Fördelning:* inte mindre än cirka 25 % och inte mer än cirka 75 % i stamaktier, resten i obligationer av hög kvalitet; 50/50 som standard; justera när proportionerna glider.
2. *Spridning:* cirka 10 till 30 olika bolag.
3. *De sju testen* (bolag i industri; siffrorna är från 1973): tillräcklig storlek; stark finansiell ställning (omsättningstillgångar minst dubbelt så stora som kortfristiga skulder, långfristig skuld högst lika med nettoomsättningstillgångar); vinst i var och en av de senaste tio åren; obruten utdelning i tjugo år; vinsttillväxt på minst en tredjedel över tio år (treårsmedel i början och slutet); pris högst cirka 15 gånger snittvinsten de senaste tre åren; pris högst cirka 1,5 gånger bokfört värde, eller P/E gånger P/B högst 22,5.
4. *För den företagsamma:* net-nets (pris under två tredjedelar av nettoomsättningstillgångarna) och tydliga fynd.
5. *Säkerhetsmarginal* mellan konservativt värde och pris, och en marknad som är en nyckfull affärspartner vars prisbud man får ta eller strunta i.

**Portfölj.** Ingen. Han dog 1976. Karaktärens "referensportfölj" är därför reglerna ovan, tydligt märkta som 1973 års upplaga. De sju testen kräver tioåriga historier som leverantörerna inte alltid har; karaktären ska säga "kan inte testas" och inte gissa.

**Vad karaktären har fått.** Beslutsordning och regler; testen som kontrollerar; strategin `teacher_defensive` (rankning). Rankningen är bara en faktorapproximation (värde, kvalitet och låg volatilitet) och kan inte pröva tioårsvinster eller utdelningshistorik.

### 3.3 Peter Lynch, karaktären Marty Fairbanks (`pickerupper`)

**Källa.** *One Up on Wall Street* (1989) och *Beating the Street* (1993), samt sekundärkällor (Wikipedia, GuruFocus, tidningsartiklar) för siffror.

**Arbetssätt.**

1. *Börja med det du känner till.* En produkt, butik eller bransch som du förstår bättre än marknaden.
2. *Sex kategorier:* slow growers, stalwarts (cirka 10–12 % tillväxt), fast growers (20–25 %), cyclicals, turnarounds, asset plays. Varje kategori bedöms med egna mått och säljs av egna skäl.
3. *Tvåminutersberättelsen:* varför tjänar bolaget mer om några år, och vad måste gå rätt?
4. *Kontrollera siffrorna mot berättelsen.* Ett rimligt prissatt bolag har ett P/E ungefär lika med tillväxttakten; hälften är mycket bra, dubbelt är mycket dåligt (PEG).
5. *Balansräkning och varulager:* skuld mot eget kapital, nettokassa, lager som växer snabbare än försäljningen, insiders och bolagets egna köp.
6. *Följ historien:* sälj när berättelsen eller siffrorna ändras, inte när kursen faller.

**Portfölj.** Ingen att citera i dag. Hans fond Magellan avkastade cirka **29,2 % per år 1977–1990** och slog index de flesta åren (sekundärkälla). Fonden hade över tusen innehav när han lämnade (sekundärkälla, och de olika källorna går isär). I hans egen redogörelse, citerad av GuruFocus, köpte han Fannie Mae tills den var 2,1 % av fonden; en tidningslista från 1990 nämner Fannie Mae, Philip Morris, General Electric, Nestlé och MCI bland de största. Bolag i hans böcker är **exempel på metoden**, inte innehav.

**Vad karaktären har fått.** Beslutsordning, kategorier, PEG-regeln, kontrollerna, portföljreglerna för en privatperson (en handfull bolag som man hinner följa; blanda kategorier) och strategin `pickerupper_garp` (rankning). Tvåminutersberättelsen och "vad du kan kolla i verkligheten" går inte att göra ur data; karaktären ber användaren göra det.

### 3.4 Jim Simons, karaktären Dr. Isak Voss (`quant`)

**Källa.** Gregory Zuckerman, *The Man Who Solved the Market* (2019), samt sekundära återgivningar av Medallion-fondens avkastning. Renaissance Technologies har aldrig redovisat sina metoder, och grundaren dog i maj 2024.

**Dokumenterad filosofi (bok, sekundärt).** Låt data avgöra; anställ forskare snarare än finansfolk; testa mönster på mycket data; många små och oberoende satsningar; kostnader och överanpassning äter de flesta synbara fördelar; sannolikheter i stället för övertygelse. Fondens avkastning anges ofta till cirka 66 % brutto och 39 % netto per år 1988–2018. Det är en sekundärkälla, ej reviderat, och vissa ifrågasätter hur det är möjligt.

**Portföljen (SEC 13F-HR, 2026-06-30, accession 0001037389-26-000059).** **3 138 positioner**, 72,6 miljarder dollar.

| Mått | Värde |
| --- | --- |
| Största position (NVIDIA) | 1,95 % |
| Fem / tio största | 8,1 % / 12,8 % |
| Herfindahl-index | 35 (mycket spridd) |

Förändringarna är enorma från ett kvartal till nästa (NVIDIA +181 %, Alphabet +424 %, Meta +699 % i antal aktier mot Q1), vilket visar hög omsättning: ögonblicksbilden säger nästan inget om strategin.

**Vad karaktären har fått.** Beslutsordning (hypotes, test utanför urvalet, kostnader, sannolikhet med felmarginal, riskstorlek, nedgång), principen att ingen position är stor (siffrorna ovan), kravet på hundratals oberoende observationer, och strategierna `persona_quant_breakout`, `persona_quant_meanrev` och `momentum`. Karaktären har uttryckligen **inga hemliga modeller**.

**Vad vi inte kan återskapa.** Själva signalerna, datan och exekveringen.

### 3.5 Larry Fink, karaktären Margit Lindholm (`allocator`)

**Källa.** Finks brev för 2026 (BlackRock, publicerat i mars 2026; läst direkt), BlackRock Investment Institutes utsikter (sekundärkälla), och BlackRock, Inc:s 13F.

**Vad han säger.**

- *Håll dig investerad.* "Staying invested has mattered far more than getting the timing right"; att missa de tio bästa dagarna sänker avkastningen kraftigt.
- *Bygg grunden först:* sparbuffert innan placeringar, sedan pensions- och skattekonton.
- *Sprid* över många tillgångar till låg kostnad; se på infrastruktur och privat kredit (och på sikt tokenisering) som sätt att nå dem.
- *AI* skapar stort ekonomiskt värde; frågan är vem som får del av det. Infrastrukturen (datacenter, kraft, nät) behöver yrkesarbetare.
- *Energipragmatism:* "Affordable energy depends on abundant energy", från olja och gas till förnybart, kärnkraft och lagring.
- *Sekundärt (BlackRock Investment Institute):* fem "mega forces" (digital disruption och AI, demografisk divergens, geopolitisk fragmentering, framtidens finanser, lågkolsomställningen); övervikt i AI-temat; undervikt i långa amerikanska statsobligationer; intresse för privat kredit och infrastruktur; "ingen neutral hållning" i en värld som styrs av få krafter.

**Portföljen (SEC 13F-HR, BlackRock, Inc., 2026-06-30, accession 0002012383-26-003238).** **5 650 positioner**, **6,7 biljoner dollar**. Fem största: NVIDIA 5,8 %, Apple 5,0 %, Alphabet 4,4 %, Microsoft 3,4 %, Amazon 2,7 % (fem största 21,2 %; Herfindahl 128). Det är i praktiken *marknaden*: tabellen sammanfattar kundernas indexfonder och är **inte** husets åsikt. Det står i karaktärens förbehåll.

**Vad karaktären har fått.** Beslutsordning från målet och grunden till strukturella teman; "portföljnivå före enskilt bolag"; krav på likviditet och kostnad; att tolka referensportföljen som marknadsvikter; strategin `persona_allocator_trend` (200-dagarsfilter) och leaderboard `quality`.

### 3.6 Cathie Wood, karaktären Cora Whitfield (`innovator`)

**Källa.** ARK:s egen beskrivning av investeringsprocessen (ARK Help Center, läst direkt) och ARKK:s publicerade dagliga innehav.

**Processen (primär).**

- *Top-down:* har möjligheten tillväxt som ger premie mot marknaden, och är den investerbar? Man ser flerårsomvandlingar av hela värdekedjor, modellerar kostnadskurvor (Wrights lag) och efterfrågeelasticitet. Varaktighet på minst tre till fem år.
- *Bottom-up:* underlag, värderingsmodeller och poäng på sex mått: ledning och kultur, leverans mot uttalade mål, konkurrensfördel, produkt- och tjänsteledarskap, risk i investeringscaset och värdering.
- *Värderingstest:* en avkastningströskel på **15 % per år över fem år**.
- *Aktiv:* övertygelser diskuteras varje vecka. (Sidan beskriver inte hur övertygelse blir positionsstorlek eller hur försäljning beslutas.)

**Portföljen (ARKK, daglig fil, 2026-11-02).** **46 positioner**, 6,2 miljarder dollar. Största: Tesla 11,2 %, SpaceX 6,1 %, Tempus AI 5,7 %, CRISPR Therapeutics 4,5 %, Circle 4,5 %, en icke börsnoterad post i OpenAI 4,0 %, Coinbase 3,8 %, Twist Bioscience 3,3 %, Shopify 3,1 %, NVIDIA 3,0 %. Tio största **49,2 %**, Herfindahl 387. Fonden innehåller även privata bolag (OpenAI, Kalshi) och en statsskuldsfond som kassa.

**Sekundärt.** Flera källor anger att ARK:s totala avkastning sedan 2014 ligger nära indexets, medan ARKK:s femårsavkastning var negativ till början av 2026, och att ARK:s tidigare femårsprognoser (40–50 % per år) inte infriades. Siffrorna är från sekundärkällor och bör kontrolleras innan de citeras.

**Vad karaktären har fått.** Beslutsordning och de sex poängen, kravet att visa bear-, bas- och bull-fall mot 15 %-tröskeln, positionsform (koncentrerad, aktiv, hög volatilitet, kassa, onoterat), krav på att visa nedsidan, och strategierna `persona_innovator_leader` och `growth`.

## 4. Jämförelse

| | Horisont | Antal positioner | Största | Tio största | Omsättning | Vem äger beslutet |
| --- | --- | --- | --- | --- | --- | --- |
| Berkshire (Buffett) | decennier | 27 | 22,0 % | 91,0 % | låg | Få personer |
| Graham | år | 10–30 (regel) | – | – | låg | Investeraren |
| Lynch | år | en handfull (privatperson) | – | – | medel | Investeraren |
| Renaissance (Simons) | dagar till veckor | 3 138 | 1,95 % | 12,8 % | mycket hög | Modeller |
| BlackRock (Fink) | decennier | 5 650 | 5,8 % | 29,4 % | låg (index) | Kunderna och index |
| ARKK (Wood) | cirka 5 år | 46 | 11,2 % | 49,2 % | hög | Aktiv förvaltare |

Skillnaden i *portföljens form* är det mest åtkomliga i materialet, och det är det karaktärerna får med sig: koncentration och tålamod hos Ashgrove, spridning hos Voss, indexlikhet hos Lindholm och koncentrerad innovation hos Whitfield.

## 5. Hur karaktärerna använder materialet

- **Verktyget `get_reference_portfolio`** (för agenter, flöden och MCP): ger en karaktärs redovisade portfölj med filer, datum, koncentration och största positioner, och slår upp om en ticker finns med och hur stor den är. Det svarar alltid med att det är en redovisning och inte en position.
- **Leaderboard-universum** `ref_owner`, `ref_quant`, `ref_allocator` och `ref_innovator`: de 25 största positionerna med ticker i varje redovisad portfölj. Att rangordna ett bolag mot en karaktärs egna innehav visar likheten. En backtest på en sådan lista ser tillbaka genom bolag som någon valde att äga och **överdriver historien** (hindsight bias). Det står i universums beskrivning.
- **Profilpanelen** på sidan Agent team visar process, portföljform, referensportfölj med datum och förbehåll, och källor, per karaktär.
- **Prompten** nämner referensportföljen med datum och säger att den ska kallas en redovisning och inte en vy.

## 6. Förnya data

```
python3 scripts/refresh_reference_portfolios.py "Ditt Namn din@epost.se"
```

Skriptet hämtar senaste och föregående 13F-HR för Berkshire (CIK 1067983), Renaissance (1037389) och BlackRock, Inc. (2012383) samt ARKK:s dagliga fil, och skriver `internal/persona/data/reference_portfolios.json`. Ange en riktig kontakt: SEC kräver den i User-Agent. Ett CUSIP som saknas i `CUSIP_TICKERS` får inget tickernamn tills det läggs till. Nästa 13F för kvartalet som slutar 2026-09-30 ska lämnas in senast den 14 november 2026.

## 7. Det du bör veta

- Ingen av karaktärerna är en verklig person, och ingen ger råd. De mimickar *ett sätt att tänka* och visar *en dagsfärsk redovisning*.
- Källorna är av olika kvalitet. Berkshires brev och ägarmanual, ARK:s process, Finks brev och SEC-uppgifterna är primärkällor. Graham och Lynch bygger på böcker som inte kunnat kontrolleras online här. Simons metoder är i grunden okända; det som står är filosofi enligt en bok.
- 13F och en kvartalsbild är dåliga underlag för *varför* och *när*. Det som går att göra är att jämföra ett bolags form och storlek med portföljens form, och att köra karaktärens egna strategier som test.
- En strategi som testas på dagens innehav ser tillbaka på vinnare. Använd universumen som jämförelse, inte som bevis.

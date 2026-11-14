---
title: Collections
order: 3.5
summary: Keep your own sets of instruments, such as a watch list per theme or a hypothetical model portfolio, and see what each one is made of.
last-reviewed: 2026-11-14
---

# Collections

A collection is a named set of instruments that belongs to you: a Technology watch list, a Medicine research group, a hypothetical model portfolio. Collections are stored in FloweySales on the server, not in your browser, so they follow you to every device you sign in on. Nobody else can see or change them, administrators included, through the app.

Open **Collections** in the navigation.

## Kinds

You choose the kind when you create a collection. It cannot be changed later.

| Kind | Use it for | Weights |
| --- | --- | --- |
| Watch collection | Instruments you follow for monitoring and research | None. The dashboard counts instruments; it never shows an allocation. |
| Model portfolio | A hypothetical allocation you want to think through | Optional, in percent. Each weight is above 0, and together they never exceed 100%. |

A model portfolio is not a holding and not advice. Nothing is bought or sold.

## Create and organize

1. Select **New collection**.
2. Give it a name (unique among your collections, ignoring case), pick the kind, and optionally a category, tags (separated by commas) and a description.
3. Select **Create**. The new collection opens.

The list on the left can be searched and filtered by kind, category and tag, and sorted by name, last change, creation or number of instruments. **Archive** hides a collection from the list without deleting it; choose **Show: Archived** to find it again and **Restore** it. **Delete** asks first and cannot be undone.

## Add instruments

In an open collection, under **Add instruments**:

- **Add selected instrument** adds the instrument currently selected in [Markets](markets.md).
- **Search** finds instruments through a data provider. Pick the provider, type a name or symbol, and select **Add** next to a result.
- **Add a symbol by hand** takes a provider and a symbol, for example `YAHOO` and `VOLV-B.ST`. Indices (`^GSPC`) and futures (`GC=F`) are accepted.

An instrument is identified by its data provider and symbol. The same ticker from two providers, or on two exchanges, stays two separate instruments; FloweySales never merges them because the ticker matches. Name, type and exchange are kept when the search result has them. Nothing is filled in when it is not known.

## Notes, tags and weights

Select **Edit** on an instrument row to add a note and tags. In a model portfolio you can also set a hypothetical weight; the form says why a weight is refused, for example when the total would exceed 100%.

**Copy or move to** puts the instrument in another of your collections. The note and tags go along; a weight does not, because it only means something inside its own model portfolio.

**Open in Markets** shows the instrument's chart and data.

## The dashboard

An open collection shows:

- **Instruments**: how many it has.
- For a watch collection, **Data providers**: how many providers the instruments come from.
- For a model portfolio, **Weighted** and **Unallocated**: the summed weights and what is left to 100%. Unallocated is shown as its own row, never spread over the instruments.
- **Composition** by asset type, exchange, currency, country or data provider, as a table with bars. For a watch collection the bars are counts of instruments. For a model portfolio they are summed weights.

Below the composition a coverage line says how many instruments the chosen dimension is known for. Instruments where it was not stored are shown as **Unknown**, not guessed.

## Profiles and classification

The **Classification** card groups the collection by **Asset class**, **Sector**, **Region** or **Theme**, with the same counts or weights as the composition. Instruments without a value are shown as **Unclassified**. One instrument can have several themes, so theme shares can add up to more than 100%, and themes are always counted, never weighted.

The classes come from two places, in this order:

1. **Your classification**, if you set one for the instrument.
2. **The instrument's provider profile**: what its own data provider states about it, such as sector, industry and country. The region follows the country.

Opening a collection only reads profiles FloweySales already has; it does not contact any provider. Select **Fetch profiles** to ask the providers for missing or expired profiles. Large collections are fetched in rounds. The coverage line then says how many instruments have a profile, how many have not been fetched, how many their provider could not find, and how many are on a provider that offers no profiles.

A profile always comes from the instrument's own provider. When that provider offers no profiles, the instrument has none: FloweySales never copies another provider's profile onto it, even when the ticker is the same. You can still classify it yourself. When a provider's wording does not match a FloweySales class, for example an unusual sector name, it is shown as it is, marked **unmapped**, and counted as unclassified.

### Instrument details

In **Instrument classes**, select **Details** on a row to see:

- the provider profile: name, description and facts such as ISIN, exchange, market capitalization or a fund's expense ratio, whatever the provider stated;
- where it came from: the provider, the as-of time if the provider gave one, when FloweySales retrieved it (in UTC), and a link to the provider's page when there is one. **Refresh profile** asks the provider again;
- **Your classification**: asset class, sector, industry, a two-letter country code and up to 10 themes. Empty fields keep the provider's value. **Save classification** stores it for you only; nobody else sees it, and the provider's profile is never changed. **Reset to provider** removes it.

Values you set yourself are highlighted in the table. Hover a value to see its source.

### Identity

When two instruments in a collection share an ISIN, the **Identity** card says what that means:

- **Same listing**: the same ISIN on the same exchange, for example one share from two providers.
- **Same security, other venue**: the same ISIN on another or an unknown exchange.
- **Same ticker, unconfirmed** and **Different instruments**: a shared ticker without a matching ISIN.

Each link gives its reason. Instruments are never merged: they stay separate rows, with their own provider data.

## Export and import

**Export** downloads all your collections as a JSON file named with today's UTC date. **Import** reads such a file. An import always creates new collections: when a name is taken the collection is saved under a new name, for example "Energy (2)", and the result lists what was created. Nothing is merged into an existing collection, and the whole file is checked before anything is saved.

## Limits

- At most 200 collections, 1,000 instruments per collection and 20 tags per collection or instrument.
- Profiles depend on the installed data provider plugins. A provider without the profile capability has no profiles.
- Collections are not yet a scope for news, the Research Agent or Flow Studio. These are planned.
- Linked portfolio views, derived from real positions, are not available.

The technical design is in [Market Collections](../market-collections.md).

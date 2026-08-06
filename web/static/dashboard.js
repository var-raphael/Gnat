// Gnat dashboard controller.
//
// MOCK MODE: every loader below reads from /mock-data.json instead of the
// real /api/stats/* endpoints. This is deliberate — the frontend is being
// designed first so the mock payload shape becomes the contract the real
// backend implements. Each loader is written the way it will look once
// live: swap the fetch path (see MOCK_DATA_URL) and, where noted, remove
// the client-side aggregation once a dedicated backend endpoint exists.
//
// Also assumes the dashboard is served from the same origin as the API
// (embedded in the same binary), so no API key is configured here. If the
// dashboard is ever split from the API server, this needs its own auth
// story — flagged, not silently assumed.

const MOCK_DATA_URL = "/mock-data.json";
const TIERS_URL = "/country-tiers.json";
// Real version: POST /api/ai-review/regenerate (or similar) and return the
// new { generated_at, model, summary } object. No such endpoint exists
// yet, so regeneration in mock mode re-reads the same static payload and
// just re-stamps generated_at, with an artificial delay so the loading
// state is visibly testable rather than instant.
const AI_REVIEW_REGENERATE_URL = "/mock-data.json";

// Chart.js instances live here, OUTSIDE Alpine's reactive x-data object.
// Alpine wraps everything in x-data with a reactive Proxy so it can detect
// changes — but a live Chart.js instance is a large mutable object full of
// canvas contexts, internal caches, and circular references. Letting Alpine
// proxy that object invites subtle breakage (Alpine trying to "watch" deep
// internals it has no business touching). Keeping instances in a plain,
// non-reactive object sidesteps that entirely — this mirrors how the old
// HTMX version worked, where each chart was just a local variable with no
// framework wrapping it at all.
const chartRegistry = {
	visitors: null,
	donut: null,
	retention: null,
};

// Replaces a canvas with a brand-new one carrying a fresh id, instead of
// reusing the same canvas element across renders. This is the same trick
// the old HTMX version used (uniqid() per chart) — a fresh canvas avoids
// any possibility of stale sizing, stale context state, or a lingering
// reference from a previous Chart.js instance that didn't clean up right.
function freshCanvas(oldCanvas) {
	const replacement = document.createElement("canvas");
	replacement.id = oldCanvas.id;
	replacement.className = oldCanvas.className;
	oldCanvas.replaceWith(replacement);
	return replacement;
}

function gnatDashboard() {
	return {
		theme: "dark",
		range: "7", // "7" | "30" | "90" | "custom"
		activeTab: "overview", // overview | funnels | retention

		// custom date range popover
		customRangeOpen: false,
		customFrom: "",
		customTo: "",
		appliedCustomFrom: null,
		appliedCustomTo: null,

		// stat cards
		stats: {},

		// chart + donut
		visitorsOverTime: { points: [] },
		trafficSources: [],

		// breakdown widgets
		topPages: [],
		topReferrers: [],
		countries: [],
		devices: [],
		browsers: [],
		customEvents: [],

		// live visitors
		liveVisitors: [],

		aiReview: null,
		aiReviewLoading: false,

		// funnels
		funnels: [],
		selectedFunnelId: null,

		// retention
		retention: { points: [] },

		// modal
		modalOpen: false,
		modalTitle: "",
		modalRows: [],
		modalKind: "", // drives icon/flag rendering inside the modal

		// event property drill-down modal
		eventDetailOpen: false,
		eventDetail: null, // the customEvents row (with .properties) currently shown

		// export
		exportFormat: "csv", // "csv" | "json" | "jsonl"
		rawData: null, // full last-loaded payload, kept as-is for export

		// mcp
		mcpCopied: "", // "" | "config" | "query" — which code block was just copied

		init() {
			const saved = localStorage.getItem("_gnat_theme");
			this.theme = saved || "dark";
			this.loadAll();
		},

		toggleTheme() {
			this.theme = this.theme === "dark" ? "light" : "dark";
			localStorage.setItem("_gnat_theme", this.theme);
			if (chartRegistry.visitors) this.renderChart();
			if (chartRegistry.donut) this.renderDonut();
			if (chartRegistry.retention) this.renderRetentionChart();
		},

		setPresetRange(days) {
			this.range = days;
			this.appliedCustomFrom = null;
			this.appliedCustomTo = null;
			this.loadAll();
		},

		openCustomRange() {
			// seed the inputs with the currently applied custom range, or
			// today/7-days-ago as a sane starting point
			const fmt = (d) => d.toISOString().slice(0, 10);
			this.customFrom = this.appliedCustomFrom || fmt(new Date(Date.now() - 7 * 86400000));
			this.customTo = this.appliedCustomTo || fmt(new Date());
			this.customRangeOpen = true;
		},

		closeCustomRange() {
			this.customRangeOpen = false;
		},

		applyCustomRange() {
			if (!this.customFrom || !this.customTo) return;
			if (this.customFrom > this.customTo) return; // silently ignore an inverted range
			this.appliedCustomFrom = this.customFrom;
			this.appliedCustomTo = this.customTo;
			this.range = "custom";
			this.customRangeOpen = false;
			this.loadAll();
		},

		// short label for the pill itself, e.g. "Feb 1 – Feb 14"
		get customRangeLabel() {
			if (!this.appliedCustomFrom || !this.appliedCustomTo) return "Custom";
			const opts = { month: "short", day: "numeric" };
			const from = new Date(this.appliedCustomFrom + "T00:00:00").toLocaleDateString("en-US", opts);
			const to = new Date(this.appliedCustomTo + "T00:00:00").toLocaleDateString("en-US", opts);
			return `${from} – ${to}`;
		},

		dateRangeParams() {
			if (this.range === "custom" && this.appliedCustomFrom && this.appliedCustomTo) {
				return `from=${this.appliedCustomFrom}&to=${this.appliedCustomTo}`;
			}
			const to = new Date();
			const from = new Date();
			from.setDate(from.getDate() - parseInt(this.range, 10));
			const fmt = (d) => d.toISOString().slice(0, 10);
			return `from=${fmt(from)}&to=${fmt(to)}`;
		},

		async fetchJSON(path) {
			try {
				const res = await fetch(path);
				if (!res.ok) return null;
				return await res.json();
			} catch (e) {
				return null;
			}
		},

		// converts a raw list into bar-chart rows with a pct field,
		// relative to the largest count in the list, so the widest bar is
		// always 100% and others scale proportionally.
		toBreakdownRows(data, labelKey, countKey) {
			if (!data || data.length === 0) return [];
			const max = Math.max(...data.map((d) => d[countKey]));
			return data.map((d) => ({
				...d,
				label: d[labelKey],
				count: d[countKey],
				pct: max > 0 ? Math.round((d[countKey] / max) * 100) : 0,
			}));
		},

		async loadAll() {
			// Real version: fire the per-widget calls in loadAll() below,
			// each against its own /api/stats/... endpoint with
			// this.dateRangeParams(). Mock version loads one payload once,
			// since the mock file isn't range-aware yet.
			const [data, tiers] = await Promise.all([
				this.fetchJSON(MOCK_DATA_URL),
				this.fetchJSON(TIERS_URL),
			]);
			if (!data) return;

			this.tierMap = (tiers && tiers.tiers) || {};
			this.rawData = data;

			this.loadStats(data.stats);
			this.loadVisitorsOverTime(data.visitors_over_time);
			this.loadTrafficSources(data.traffic_sources);
			this.loadTopPages(data.top_pages);
			this.loadTopReferrers(data.top_referrers);
			this.loadCountries(data.countries);
			this.loadDevices(data.devices);
			this.loadBrowsers(data.browsers);
			this.loadCustomEvents(data.custom_events);
			this.loadLiveVisitors(data.live_visitors);
			this.loadAIReview(data.ai_review);
			this.loadFunnels(data.funnels);
			this.loadRetention(data.retention);
		},

		loadStats(stats) {
			if (!stats) return;
			this.stats = stats;
		},

		formatDuration(seconds) {
			const m = Math.floor(seconds / 60);
			const s = seconds % 60;
			return `${m}m ${s}s`;
		},

		formatDelta(pct) {
			if (pct === undefined || pct === null) return null;
			const sign = pct > 0 ? "+" : "";
			return `${sign}${pct.toFixed(1)}% from yesterday`;
		},

		// Waits for Chart.js to be available on window before running `fn`.
		// Chart.js is loaded via a separate <script defer> tag from the CDN;
		// under slow networks or unlucky ordering, Alpine's init() can run
		// and try to draw a chart before that script has finished executing.
		// Rather than silently giving up (which left charts blank forever),
		// retry on a short interval until Chart shows up, with a sane cap
		// so a genuinely broken/blocked CDN doesn't retry forever.
		_waitForChartJS(fn, attempt = 0) {
			if (typeof Chart !== "undefined") {
				// Two nested rAFs guarantee the browser has finished a full
				// layout/paint cycle before we measure canvas dimensions.
				// One rAF alone can still land mid-layout in some browsers;
				// this is the standard belt-and-suspenders fix for canvases
				// that measure as 0x0 on first attempt.
				requestAnimationFrame(() => requestAnimationFrame(() => fn()));
				return;
			}
			if (attempt >= 40) {
				console.error("Gnat dashboard: Chart.js failed to load after waiting; charts will not render.");
				return;
			}
			setTimeout(() => this._waitForChartJS(fn, attempt + 1), 100);
		},

		loadVisitorsOverTime(series) {
			if (!series) return;
			this.visitorsOverTime = series;
			this.$nextTick(() => this._waitForChartJS(() => this.renderChart()));
		},

		renderChart() {
			let canvas = document.getElementById("visitors-chart");
			if (!canvas || typeof Chart === "undefined") return;

			// Destroy any previous instance BEFORE swapping the canvas —
			// Chart.js keeps internal listeners tied to the old element.
			if (chartRegistry.visitors) {
				chartRegistry.visitors.destroy();
				chartRegistry.visitors = null;
			}

			// Swap in a brand-new canvas rather than reusing the existing
			// element. This matches the pattern from the working HTMX
			// version of this dashboard, where every chart render got a
			// fresh canvas with a fresh id — no leftover sizing, context
			// state, or event bindings carried over from a previous render.
			canvas = freshCanvas(canvas);

			// Give the canvas an explicit starting CSS size (not a pixel
			// buffer size) so Chart.js has something non-zero to measure on
			// the very first render, in case the wrapper is still 0x0 for a
			// frame. This is CSS sizing only (canvas.style.width/height) —
			// we deliberately do NOT also set canvas.width/height here.
			//
			// Root cause of the "font grows every time you revisit this
			// tab" bug: `responsive: true` below makes Chart.js attach a
			// ResizeObserver to the canvas's parent. Every time the
			// Overview tab's x-show flips the container from display:none
			// back to visible, that observer fires and Chart.js resizes the
			// canvas's pixel buffer itself, using the page's real
			// devicePixelRatio. Manually writing canvas.width/height here
			// too created a second, competing source of truth: our manual
			// write set a 1:1 buffer-to-CSS ratio, then Chart's own
			// resize-observer pass ran afterward and re-derived the buffer
			// size using the *actual* DPR (the `devicePixelRatio: 1` chart
			// option only affects Chart's internal scaling math, not what
			// the browser's ResizeObserver reports) — inflating the buffer
			// relative to the CSS size a little more on every tab
			// revisit, which is what stretched the fonts upward each time.
			// Removing the manual canvas.width/height write and letting
			// Chart.js's own responsive handling be the *only* thing that
			// sets the pixel buffer eliminates that double-write entirely.
			const wrap = canvas.closest(".chart-wrap") || canvas.parentElement;
			const w = Math.floor(wrap.clientWidth) || 400;
			const h = Math.floor(wrap.clientHeight) || 220;
			canvas.style.width = w + "px";
			canvas.style.height = h + "px";

			const styles = getComputedStyle(document.body);
			const accent = styles.getPropertyValue("--accent").trim();
			const accentFaint = styles.getPropertyValue("--accent-faint").trim();
			const border = styles.getPropertyValue("--line") || styles.getPropertyValue("--border");
			const muted = styles.getPropertyValue("--muted").trim();

			const points = this.visitorsOverTime.points || [];

			chartRegistry.visitors = new Chart(canvas, {
				type: "line",
				data: {
					labels: points.map((p) => p.label),
					datasets: [
						{
							data: points.map((p) => p.count),
							borderColor: accent,
							backgroundColor: accentFaint,
							borderWidth: 2,
							pointRadius: 2,
							pointBackgroundColor: accent,
							tension: 0.3,
							fill: true,
						},
					],
				},
				options: {
					responsive: true,
					maintainAspectRatio: false,
					// Canvas pixel dimensions are already set manually above
					// (canvas.width/height, in CSS px) so Chart.js doesn't
					// have to guess a starting size on a canvas that might
					// still be 0x0. Pinning devicePixelRatio to 1 stops
					// Chart.js's own responsive resize handler from ALSO
					// multiplying that manual size by the screen's DPR on
					// top of it — without this, switching tabs away and
					// back re-triggered Chart's resize observer against a
					// canvas whose width attribute and CSS style width no
					// longer agreed, and each pass compounded the scale
					// factor a little further, which is what made chart
					// text look larger every time you returned to Overview.
					devicePixelRatio: 1,
					plugins: { legend: { display: false } },
					scales: {
						x: {
							grid: { display: false },
							ticks: { color: muted, font: { family: "JetBrains Mono", size: 10 } },
						},
						y: {
							beginAtZero: true,
							grid: { color: border.toString().trim() || "rgba(255,255,255,0.08)" },
							ticks: { color: muted, font: { family: "JetBrains Mono", size: 10 } },
						},
					},
				},
			});
		},

		loadTrafficSources(sources) {
			if (!sources) return;
			const total = sources.reduce((sum, s) => sum + s.count, 0);
			this.trafficSources = sources.map((s) => ({
				...s,
				pct: total > 0 ? Math.round((s.count / total) * 100) : 0,
			}));
			this.$nextTick(() => this._waitForChartJS(() => this.renderDonut()));
		},

		renderDonut() {
			let canvas = document.getElementById("traffic-donut");
			if (!canvas || typeof Chart === "undefined") return;

			if (chartRegistry.donut) {
				chartRegistry.donut.destroy();
				chartRegistry.donut = null;
			}

			canvas = freshCanvas(canvas);

			// See the matching comment in renderChart() — measure the
			// stable .donut-wrap container (fixed size in CSS) rather than
			// canvas.parentElement, which can be transiently inflated by
			// the just-replaced canvas right after freshCanvas() runs.
			const wrap = canvas.closest(".donut-wrap") || canvas.parentElement;
			const w = Math.floor(wrap.clientWidth) || 180;
			const h = Math.floor(wrap.clientHeight) || 180;
			canvas.width = w;
			canvas.height = h;
			canvas.style.width = w + "px";
			canvas.style.height = h + "px";

			chartRegistry.donut = new Chart(canvas, {
				type: "doughnut",
				data: {
					labels: this.trafficSources.map((s) => s.label),
					datasets: [
						{
							data: this.trafficSources.map((s) => s.count),
							backgroundColor: this.trafficSources.map((s) => s.color),
							borderWidth: 0,
						},
					],
				},
				options: {
					responsive: true,
					maintainAspectRatio: false,
					// See the matching comment in renderChart() above — pins
					// the resolution to the manually-set canvas size so
					// repeated destroy/recreate cycles (e.g. leaving and
					// returning to the Overview tab) can't compound a DPR
					// scale factor on top of itself.
					devicePixelRatio: 1,
					cutout: "68%",
					plugins: { legend: { display: false } },
				},
			});
		},

		loadTopPages(pages) {
			if (!pages) return;
			this.topPages = this.toBreakdownRows(pages, "path", "count");
		},

		loadTopReferrers(referrers) {
			if (!referrers) return;
			// Only "referral" category rows belong here — Direct, Google,
			// Social, and Email already have their own slice in the
			// Traffic Sources donut above, so repeating them in this list
			// would double-count the same visitors under two widgets.
			// This list exists to answer "which *other sites* send us
			// traffic," which is only meaningful for the referral bucket.
			const referralOnly = referrers.filter((r) => r.category === "referral");
			this.topReferrers = this.toBreakdownRows(referralOnly, "referrer", "count");
		},

		loadCountries(countries) {
			if (!countries) return;
			this.countries = countries; // already has count/pct/tier from mock
		},

		loadDevices(devices) {
			if (!devices) return;
			this.devices = devices;
		},

		// Browser bucketing itself happens server-side (or in mock-data.json).
		// This just passes through whatever labels arrive — if "Other" is
		// still swallowing a lot of traffic, expand the browser-detection
		// list at the source (wherever the user-agent gets parsed into a
		// name), not here; this function has no classification logic to
		// widen.
		loadBrowsers(browsers) {
			if (!browsers) return;
			this.browsers = browsers;
		},

		loadCustomEvents(events) {
			if (!events) return;
			this.customEvents = this.toBreakdownRows(events, "event_name", "count");
		},

		loadLiveVisitors(visitors) {
			if (!visitors) return;
			this.liveVisitors = visitors;
		},

		loadAIReview(review) {
			if (!review) return;
			this.aiReview = review;
		},

		// Re-requests the AI summary against the current data. Mock mode
		// re-reads the same static payload (there's no variation to show)
		// but still exercises the real request/loading/error path so the
		// UI is already correct once a live regenerate endpoint exists —
		// swap AI_REVIEW_REGENERATE_URL for a POST to that endpoint and
		// this method doesn't need to change shape.
		async regenerateAIReview() {
			if (this.aiReviewLoading) return;
			this.aiReviewLoading = true;
			try {
				const data = await this.fetchJSON(AI_REVIEW_REGENERATE_URL);
				if (data && data.ai_review) {
					// Mock has no real variation, so re-stamp generated_at
					// to now to at least reflect that a refresh happened.
					this.aiReview = { ...data.ai_review, generated_at: new Date().toISOString() };
				}
			} finally {
				this.aiReviewLoading = false;
			}
		},

		// ---- tabs --------------------------------------------------------

		setTab(tab) {
			this.activeTab = tab;
			// Chart.js canvases render to 0-size if drawn while their tab
			// is display:none, so (re)render on the frame after the tab
			// becomes visible rather than at load time.
			this.$nextTick(() => {
				if (tab === "retention") this._waitForChartJS(() => this.renderRetentionChart());
				if (tab === "overview") {
					this._waitForChartJS(() => {
						this.renderChart();
						this.renderDonut();
					});
				}
			});
		},

		// ---- funnels -------------------------------------------------

		loadFunnels(funnels) {
			if (!funnels || funnels.length === 0) return;
			this.funnels = funnels.map((f) => ({
				...f,
				steps: this.withFunnelDropoff(f.steps),
			}));
			this.selectedFunnelId = this.funnels[0].id;
		},

		// annotates each step with pct-of-first-step (bar width) and
		// pct-dropped since the previous step (the number people actually
		// want: "how many did we lose here").
		withFunnelDropoff(steps) {
			if (!steps || steps.length === 0) return [];
			const first = steps[0].count;
			return steps.map((s, i) => {
				const pctOfFirst = first > 0 ? Math.round((s.count / first) * 100) : 0;
				let dropPct = null;
				if (i > 0) {
					const prev = steps[i - 1].count;
					dropPct = prev > 0 ? Math.round(((prev - s.count) / prev) * 100) : 0;
				}
				return { ...s, pctOfFirst, dropPct };
			});
		},

		get selectedFunnel() {
			return this.funnels.find((f) => f.id === this.selectedFunnelId) || null;
		},

		get funnelOverallConversion() {
			const f = this.selectedFunnel;
			if (!f || f.steps.length === 0) return 0;
			const first = f.steps[0].count;
			const last = f.steps[f.steps.length - 1].count;
			return first > 0 ? Math.round((last / first) * 100) : 0;
		},

		// ---- retention -------------------------------------------------

		loadRetention(retention) {
			if (!retention) return;
			this.retention = retention;
			// only render immediately if the retention tab happens to
			// already be active (e.g. after a data reload); otherwise
			// setTab() triggers the render when the user switches to it.
			if (this.activeTab === "retention") {
				this.$nextTick(() => this._waitForChartJS(() => this.renderRetentionChart()));
			}
		},

		renderRetentionChart() {
			let canvas = document.getElementById("retention-chart");
			if (!canvas || typeof Chart === "undefined") return;

			if (chartRegistry.retention) {
				chartRegistry.retention.destroy();
				chartRegistry.retention = null;
			}

			canvas = freshCanvas(canvas);

			// See the matching comment in renderChart() — measure the
			// stable .chart-wrap container rather than canvas.parentElement.
			const wrap = canvas.closest(".chart-wrap") || canvas.parentElement;
			const w = Math.floor(wrap.clientWidth) || 400;
			const h = Math.floor(wrap.clientHeight) || 220;
			canvas.width = w;
			canvas.height = h;
			canvas.style.width = w + "px";
			canvas.style.height = h + "px";

			const styles = getComputedStyle(document.body);
			const accent = styles.getPropertyValue("--accent").trim();
			const accentFaint = styles.getPropertyValue("--accent-faint").trim();
			const border = styles.getPropertyValue("--line") || styles.getPropertyValue("--border");
			const muted = styles.getPropertyValue("--muted").trim();

			const points = this.retention.points || [];

			chartRegistry.retention = new Chart(canvas, {
				type: "line",
				data: {
					labels: points.map((p) => p.label),
					datasets: [
						{
							data: points.map((p) => p.pct),
							borderColor: accent,
							backgroundColor: accentFaint,
							borderWidth: 2,
							pointRadius: 3,
							pointBackgroundColor: accent,
							tension: 0.3,
							fill: true,
						},
					],
				},
				options: {
					responsive: true,
					maintainAspectRatio: false,
					// Same reasoning as renderChart()/renderDonut() — keeps
					// re-renders (e.g. leaving and returning to the
					// Retention tab) from compounding a DPR scale factor
					// on top of the manually-sized canvas.
					devicePixelRatio: 1,
					plugins: {
						legend: { display: false },
						tooltip: {
							callbacks: {
								label: (ctx) => `${ctx.parsed.y}% retained`,
							},
						},
					},
					scales: {
						x: {
							grid: { display: false },
							ticks: { color: muted, font: { family: "JetBrains Mono", size: 10 } },
						},
						y: {
							beginAtZero: true,
							max: 100,
							grid: { color: border.toString().trim() || "rgba(255,255,255,0.08)" },
							ticks: {
								color: muted,
								font: { family: "JetBrains Mono", size: 10 },
								callback: (v) => v + "%",
							},
						},
					},
				},
			});
		},

		// emoji fallback only — used when the flagcdn.com image fails to
		// load (onerror in the template). Regional-indicator emoji flags
		// render inconsistently on Android (often show as bare letters in
		// a box), which is why the image is the primary rendering path.
		countryFlagEmoji(code) {
			if (!code || code.length !== 2) return "🌐";
			const codePoints = code
				.toUpperCase()
				.split("")
				.map((c) => 127397 + c.charCodeAt(0));
			return String.fromCodePoint(...codePoints);
		},

		countryFlagUrl(code) {
			if (!code || code.length !== 2) return "";
			return `https://flagcdn.com/24x18/${code.toLowerCase()}.png`;
		},

		deviceIcon(type) {
			const icons = {
				desktop: "fa-solid fa-desktop",
				mobile: "fa-solid fa-mobile-screen-button",
				tablet: "fa-solid fa-tablet-screen-button",
				bot: "fa-solid fa-robot",
			};
			return icons[type] || "fa-solid fa-circle-question";
		},

		deviceColor(type) {
			const colors = {
				desktop: "#60a5fa",
				mobile: "#4ade80",
				tablet: "#a78bfa",
				bot: "#fb923c",
			};
			return colors[type] || "#8b9296";
		},

		// Chrome/Safari/Firefox/Edge covered most traffic, but "Other" was
		// swallowing a lot of real, nameable browsers. Expanded to cover
		// the next tier of common browsers before anything falls through
		// to the generic bucket. Colors follow each browser's real brand
		// color so the list reads at a glance, not just by icon shape.
		browserIcon(name) {
			const icons = {
				Chrome: "fa-brands fa-chrome",
				Safari: "fa-brands fa-safari",
				Firefox: "fa-brands fa-firefox-browser",
				Edge: "fa-brands fa-edge",
				Opera: "fa-brands fa-opera",
				Brave: "fa-brands fa-brave",
				Vivaldi: "fa-solid fa-v",
				"Samsung Internet": "fa-brands fa-android",
				UC: "fa-solid fa-u",
				IE: "fa-brands fa-internet-explorer",
				"Internet Explorer": "fa-brands fa-internet-explorer",
				Other: "fa-solid fa-globe",
			};
			return icons[name] || "fa-solid fa-globe";
		},

		browserColor(name) {
			const colors = {
				Chrome: "#4ade80",
				Safari: "#60a5fa",
				Firefox: "#fb923c",
				Edge: "#22d3ee",
				Opera: "#f87171",
				Brave: "#fb7185",
				Vivaldi: "#a78bfa",
				"Samsung Internet": "#60a5fa",
				UC: "#fbbf24",
				IE: "#38bdf8",
				"Internet Explorer": "#38bdf8",
				Other: "#8b9296",
			};
			return colors[name] || "#8b9296";
		},

		// Maps a referrer's known category/domain to a Font Awesome brand
		// icon and its real brand color. Anything not explicitly a
		// recognized social platform falls back to the plain web/globe
		// icon rather than a question mark — an unrecognized referring
		// site is still a website, not an error.
		referrerIcon(row) {
			const domain = (row.referrer || row.label || "").toLowerCase();
			const hit = this._referrerMatch(domain);
			return hit ? hit.icon : "fa-solid fa-globe";
		},

		referrerColor(row) {
			const domain = (row.referrer || row.label || "").toLowerCase();
			const hit = this._referrerMatch(domain);
			return hit ? hit.color : "#8b9296";
		},

		_referrerMatch(domain) {
			const socialIcons = [
				{ match: "facebook", icon: "fa-brands fa-facebook", color: "#1877f2" },
				{ match: "instagram", icon: "fa-brands fa-instagram", color: "#e1306c" },
				{ match: "twitter", icon: "fa-brands fa-twitter", color: "#1da1f2" },
				{ match: "t.co", icon: "fa-brands fa-twitter", color: "#1da1f2" },
				{ match: "x.com", icon: "fa-brands fa-x-twitter", color: "#e8e8e6" },
				{ match: "linkedin", icon: "fa-brands fa-linkedin", color: "#0a66c2" },
				{ match: "youtube", icon: "fa-brands fa-youtube", color: "#ff0000" },
				{ match: "tiktok", icon: "fa-brands fa-tiktok", color: "#25f4ee" },
				{ match: "reddit", icon: "fa-brands fa-reddit", color: "#ff4500" },
				{ match: "pinterest", icon: "fa-brands fa-pinterest", color: "#e60023" },
				{ match: "github", icon: "fa-brands fa-github", color: "#8b9296" },
				{ match: "discord", icon: "fa-brands fa-discord", color: "#5865f2" },
				{ match: "whatsapp", icon: "fa-brands fa-whatsapp", color: "#25d366" },
				{ match: "telegram", icon: "fa-brands fa-telegram", color: "#26a5e4" },
				{ match: "snapchat", icon: "fa-brands fa-snapchat", color: "#fffc00" },
				{ match: "mail", icon: "fa-solid fa-envelope", color: "#e8c07d" },
				{ match: "google", icon: "fa-brands fa-google", color: "#4285f4" },
				{ match: "bing", icon: "fa-brands fa-microsoft", color: "#00a4ef" },
				{ match: "yahoo", icon: "fa-brands fa-yahoo", color: "#720e9e" },
				{ match: "duckduckgo", icon: "fa-brands fa-searchengin", color: "#de5833" },
			];
			return socialIcons.find((s) => domain.includes(s.match));
		},

		// ---- export ------------------------------------------------------

		// Human-readable note under the format picker explaining what
		// each format actually produces, since "CSV" alone doesn't tell
		// someone whether nested sections (funnels, retention) survive
		// the conversion the same way.
		get exportFormatNote() {
			const notes = {
				csv: "One sheet per section (stats, top pages, countries, etc.), stitched into a single .csv with blank-line separators and a header row per section.",
				json: "The full payload as one structured object. Nesting (like funnel steps or retention points) is preserved exactly as shown on screen.",
				jsonl: "One JSON object per line, per section row. This is the format most log and data pipelines expect for streaming or bulk import.",
			};
			return notes[this.exportFormat] || "";
		},

		// Flattens every section of the raw payload into { sectionName:
		// rows[] } — rows are always arrays, even for single-object
		// sections like `stats`, so every downstream formatter
		// (toCSV/toJSONL) can treat all sections uniformly.
		buildExportSections() {
			const data = this.rawData;
			if (!data) return {};

			const sections = {
				stats: data.stats ? [data.stats] : [],
				visitors_over_time: data.visitors_over_time?.points || [],
				traffic_sources: data.traffic_sources || [],
				top_pages: data.top_pages || [],
				top_referrers: data.top_referrers || [],
				countries: data.countries || [],
				devices: data.devices || [],
				browsers: data.browsers || [],
				custom_events: data.custom_events || [],
				funnels: (data.funnels || []).flatMap((f) =>
					(f.steps || []).map((s) => ({ funnel_id: f.id, funnel_name: f.name, ...s }))
				),
				retention: data.retention?.points || [],
			};

			// AI review is a single freeform object, not a row-shaped
			// section — keep it out of the tabular export formats but
			// still available on the JSON export via rawData directly.
			return sections;
		},

		// Converts one section's rows into a CSV block: a header row from
		// the union of keys across all rows (since sibling rows in a
		// section can have slightly different shapes, e.g. funnel steps
		// with/without dropPct on the first step), then one line per row.
		// Values are stringified minimally and quoted only when they
		// contain a comma, quote, or newline, per standard CSV escaping.
		sectionToCSV(name, rows) {
			if (!rows || rows.length === 0) return `# ${name}\n(no data)\n`;
			const keys = Array.from(rows.reduce((set, row) => {
				Object.keys(row).forEach((k) => set.add(k));
				return set;
			}, new Set()));

			const escape = (val) => {
				if (val === null || val === undefined) return "";
				const s = String(val);
				return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
			};

			const lines = [keys.join(",")];
			for (const row of rows) {
				lines.push(keys.map((k) => escape(row[k])).join(","));
			}
			return `# ${name}\n${lines.join("\n")}\n`;
		},

		buildExportCSV() {
			const sections = this.buildExportSections();
			return Object.entries(sections)
				.map(([name, rows]) => this.sectionToCSV(name, rows))
				.join("\n");
		},

		buildExportJSON() {
			// Full fidelity: the raw payload as-is, not the flattened
			// section shape, so nested structures (funnel steps, ai
			// review) round-trip exactly as the dashboard received them.
			return JSON.stringify(this.rawData, null, 2);
		},

		buildExportJSONL() {
			const sections = this.buildExportSections();
			const lines = [];
			for (const [name, rows] of Object.entries(sections)) {
				for (const row of rows) {
					lines.push(JSON.stringify({ section: name, ...row }));
				}
			}
			return lines.join("\n");
		},

		// Builds the export in the selected format and triggers a browser
		// download via a throwaway object URL. No server round-trip —
		// mock mode already has the full payload in memory, and the real
		// version can do the same client-side once it has range-scoped
		// data, since every section is already loaded for on-screen
		// display anyway.
		downloadExport() {
			if (!this.rawData) return;

			const builders = { csv: this.buildExportCSV, json: this.buildExportJSON, jsonl: this.buildExportJSONL };
			const mime = { csv: "text/csv", json: "application/json", jsonl: "application/x-ndjson" };
			const content = builders[this.exportFormat].call(this);

			const today = new Date().toISOString().slice(0, 10);
			const filename = `gnat-export-${today}.${this.exportFormat}`;

			const blob = new Blob([content], { type: mime[this.exportFormat] + ";charset=utf-8" });
			const url = URL.createObjectURL(blob);
			const a = document.createElement("a");
			a.href = url;
			a.download = filename;
			document.body.appendChild(a);
			a.click();
			a.remove();
			// Revoke on the next tick rather than immediately — some
			// browsers cancel the download if the object URL is revoked
			// before the click's navigation has actually started.
			setTimeout(() => URL.revokeObjectURL(url), 0);
		},

		// ---- mcp -----------------------------------------------------

		async copyMcpBlock(which) {
			const blocks = {
				config: `{
  "mcpServers": {
    "gnat": {
      "url": "https://mcp.gnat.xyz/v1/sse",
      "transport": "sse"
    }
  }
}`,
				query: `Ask your agent:
"Using the gnat MCP server, get unique visitors
and top referrers for the last 7 days."`,
			};
			const text = blocks[which];
			if (!text) return;
			try {
				await navigator.clipboard.writeText(text);
				this.mcpCopied = which;
				setTimeout(() => {
					if (this.mcpCopied === which) this.mcpCopied = "";
				}, 1800);
			} catch (e) {
				// Clipboard API can fail without a secure context/permission;
				// fail quietly rather than throwing in the UI, the code
				// block itself is still selectable/copyable by hand.
			}
		},

		// ---- modal -----------------------------------------------------

		openModal(kind) {
			const map = {
				pages: { title: "Top Pages", rows: this.topPages },
				referrers: { title: "Top Referrers", rows: this.topReferrers },
				countries: { title: "Country Breakdown", rows: this.countries },
				devices: { title: "Device Types", rows: this.devices },
				browsers: { title: "Browsers", rows: this.browsers },
				events: { title: "Custom Events", rows: this.customEvents },
			};
			const entry = map[kind];
			if (!entry) return;
			this.modalKind = kind;
			this.modalTitle = entry.title;
			this.modalRows = entry.rows;
			this.modalOpen = true;
		},

		closeModal() {
			this.modalOpen = false;
		},

		// ---- event property drill-down ----------------------------------

		openEventDetail(row) {
			this.eventDetail = row;
			this.eventDetailOpen = true;
		},

		closeEventDetail() {
			this.eventDetailOpen = false;
		},

		// Converts one property's raw breakdown ([{value,count}, ...]) into
		// bar-chart rows with a pct field, same convention as
		// toBreakdownRows() above but scoped to a single property's values
		// rather than a top-level dataset.
		propertyBreakdownRows(prop) {
			if (!prop?.breakdown || prop.breakdown.length === 0) return [];
			const max = Math.max(...prop.breakdown.map((b) => b.count));
			return prop.breakdown.map((b) => ({
				...b,
				pct: max > 0 ? Math.round((b.count / max) * 100) : 0,
			}));
		},
	};
}

// ---------------------------------------------------------------------
// Tap-to-open tooltips on touch devices
//
// The [data-tooltip] system (see dashboard.css) is hover-driven, which
// doesn't exist on touch. Rather than wire an Alpine handler onto all 23+
// tooltip-bearing elements individually, this is one small delegated
// listener: tapping any [data-tooltip] element toggles a `.tooltip-open`
// class that the CSS keys off under `@media (hover: none)`; tapping
// anywhere else (or a different tooltip target) closes whatever was open.
// Lives outside the Alpine component on purpose — it's global page
// behavior, not per-instance state, and needs no reactivity.
// ---------------------------------------------------------------------
(function setupTapTooltips() {
	let openEl = null;

	function closeOpen() {
		if (openEl) {
			openEl.classList.remove("tooltip-open");
			openEl = null;
		}
	}

	document.addEventListener(
		"click",
		(e) => {
			const target = e.target.closest("[data-tooltip]");

			// Elements that already do something on click (buttons, links)
			// shouldn't also pop a tooltip on tap — that's how the AI
			// Summary regenerate button ended up flashing a stray tooltip
			// under the card every time it was pressed: it has its own
			// @click action AND a data-tooltip, and this listener was
			// treating the tap as "open the tooltip" on top of that. Real
			// interactive controls keep only their normal click behavior;
			// tap-to-open tooltips are reserved for elements whose tap has
			// no other function (stat cards, panels, funnel steps).
			if (target && target.closest("button, a, [role='button']")) {
				closeOpen();
				return;
			}

			if (target && target === openEl) {
				// second tap on the same element: close it (acts as a toggle)
				closeOpen();
				return;
			}

			if (target) {
				closeOpen();
				target.classList.add("tooltip-open");
				openEl = target;
				return;
			}

			// tapped elsewhere on the page: dismiss any open tooltip
			closeOpen();
		},
		true
	);

	document.addEventListener("keydown", (e) => {
		if (e.key === "Escape") closeOpen();
	});
})();

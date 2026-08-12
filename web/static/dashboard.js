// Gnat dashboard controller.

const TIERS_URL = "/country-tiers.json";

// Kept outside Alpine's reactive x-data — proxying live Chart.js instances causes breakage.
const chartRegistry = {
	visitors: null,
	donut: null,
	retention: null,
};

// Fresh canvas per render avoids stale Chart.js state on the old element.
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

		// auth
		// Mock only: checks against a hardcoded password shipped in this
		// file. A real version has to check this server-side against a
		// value from the env file, since anything here is visible to
		// anyone who opens dev tools, a client-side check can gate the
		// UI but can never actually secure anything on its own.
		authenticated: false,
		loginPassword: "",
		loginError: "",
		loginLoading: false,

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
		countryInfo: {}, // code -> {name, tier}, loaded from /country-tiers.json
		devices: [],
		browsers: [],
		os: [],
		customEvents: [],

		// live visitors
		liveVisitors: [],
		liveFilterField: "", // "" | "country_name" | "device"
		liveFilterValue: "", // selected value within liveFilterField
		expandedLiveGroups: [], // page paths currently expanded in the grouped view
		liveModalOpen: false,
		liveModalView: "page", // "page" | "flat" — shown inside the See All modal

		// funnels
		funnels: [],
		funnelDefs: [],
		selectedFunnelId: null,
		funnelManagerOpen: false,
		funnelEditing: null,
		funnelFormError: "",
		funnelSaving: false,
		availableEventNames: [],

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

		countryDetailOpen: false,
		countryDetailLoading: false,
		countryDetail: null, // CountryDetail payload from /api/stats/country-detail, or null while loading/closed
		expandedCountryPage: null, // path of the page whose time-on-page detail is expanded, or null

		// export
		exportFormat: "csv", // "csv" | "json" | "jsonl"

		// mcp
		mcpCopied: "", // "" | "config" | "url" | "query" — which block was just copied
		mcpHasToken: false,
		mcpTokenLoading: false,
		mcpNewToken: "", // plaintext, only ever held in memory right after generating
		mcpGenerateError: "",

		// live-refresh polling (stats + live visitors only — see startPolling)
		_pollTimer: null,
		_pollIntervalMs: 30000,
		_mcpStatusLoaded: false,

		async init() {
			const saved = localStorage.getItem("_gnat_theme");
			this.theme = saved || "dark";

			const session = await this.fetchJSON("/api/dashboard/session");
			this.authenticated = !!(session && session.authenticated);

			if (this.authenticated) {
				this.loadAll();
				this.startPolling();
			} else {
				this.$nextTick(() => this.$refs.loginInput?.focus());
			}

			// Pausing while the tab is hidden avoids burning requests (and
			// server load) on a background tab nobody's looking at — the
			// data simply catches up the moment the tab is foregrounded
			// again instead of drifting further out of date while hidden.
			document.addEventListener("visibilitychange", () => {
				if (document.hidden) {
					this.stopPolling();
				} else if (this.authenticated) {
					// Refresh immediately on return so stale background
					// time doesn't wait out a full new 30s cycle, then
					// resume the regular interval from now.
					this.pollNow();
					this.startPolling();
				}
			});
		},

		async attemptLogin() {
			this.loginLoading = true;
			this.loginError = "";
			try {
				const res = await fetch("/api/dashboard/login", {
					method: "POST",
					headers: { "Content-Type": "application/json" },
					body: JSON.stringify({ password: this.loginPassword }),
				});
				this.loginPassword = "";
				if (res.ok) {
					this.authenticated = true;
					this.loadAll();
					this.startPolling();
				} else {
					this.loginError = "Incorrect password. Try again.";
					this.$nextTick(() => this.$refs.loginInput?.focus());
				}
			} catch {
				this.loginError = "Couldn't reach the server. Try again.";
				this.$nextTick(() => this.$refs.loginInput?.focus());
			} finally {
				this.loginLoading = false;
			}
		},

		async logout() {
			this.stopPolling();
			try {
				await fetch("/api/dashboard/logout", { method: "POST" });
			} catch {
				// Best-effort: even if this fails, dropping local
				// authenticated state below still hides the dashboard.
			}
			this.authenticated = false;
			this.$nextTick(() => this.$refs.loginInput?.focus());
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
			const tiers = await this.fetchJSON(TIERS_URL);
			this.countryInfo = (tiers && tiers.countries) || {};

			this.fetchJSON(`/api/stats/funnels?${this.dateRangeParams()}`).then((rows) => {
				this.loadFunnels(rows);
			});
			this.fetchJSON(`/api/stats/live`).then((rows) => {
				this.loadLiveVisitors(rows);
			});
			this.fetchJSON(`/api/stats/summary`).then((stats) => {
				this.loadStats(stats);
			});
			this.fetchJSON(`/api/stats/custom-events?${this.dateRangeParams()}`).then((rows) => {
				this.loadCustomEvents(rows);
			});
			this.fetchJSON(`/api/stats/retention?${this.dateRangeParams()}`).then((rows) => {
				this.loadRetention(rows);
			});
			this.fetchJSON(`/api/stats/countries?${this.dateRangeParams()}`).then((rows) => {
				this.loadCountries(rows);
			});
			this.fetchJSON(`/api/stats/pageviews?${this.dateRangeParams()}`).then((rows) => {
				this.loadVisitorsOverTime(rows ? { points: rows.map((r) => ({ label: r.date, count: r.count })) } : null);
			});
			this.fetchJSON(`/api/stats/devices?${this.dateRangeParams()}`).then((rows) => {
				this.loadDevices(rows);
			});
			this.fetchJSON(`/api/stats/browsers?${this.dateRangeParams()}`).then((rows) => {
				this.loadBrowsers(rows);
			});
			this.fetchJSON(`/api/stats/os?${this.dateRangeParams()}`).then((rows) => {
				this.loadOS(rows);
			});
			this.fetchJSON(`/api/stats/pages?${this.dateRangeParams()}`).then((rows) => {
				this.loadTopPages(rows);
			});
			this.fetchJSON(`/api/stats/referrers?${this.dateRangeParams()}`).then((rows) => {
				this.loadTopReferrers(rows);
			});
			this.fetchJSON(`/api/stats/traffic-sources?${this.dateRangeParams()}`).then((rows) => {
				this.loadTrafficSources(rows);
			});
		},

		// ---- live-refresh polling ----------------------------------------
		//
		// Only `stats` (today-vs-yesterday headline numbers) and `live`
		// (visitors active right now) refresh on a timer. Every other
		// card is scoped to the selected date range (7/30/90/custom) —
		// funnels, countries, retention, etc. — and a range spanning days
		// or months simply doesn't move enough in 30s to justify an extra
		// request every cycle. Polling those too would mean ~11 requests
		// every 30s regardless of what's actually changed, for sections
		// whose numbers are effectively static within a single visit.
		// stats/live are deliberately fetched independently of loadAll's
		// other calls (not by re-running loadAll) so a poll tick never
		// re-fetches or re-renders range-scoped sections, and never
		// re-runs the tiers fetch or chart redraws tied to those.

		startPolling() {
			// guard against double-starting (e.g. a visibility toggle
			// firing while a timer is already running)
			if (this._pollTimer) return;
			this._pollTimer = setInterval(() => this.pollNow(), this._pollIntervalMs);
		},

		stopPolling() {
			if (this._pollTimer) {
				clearInterval(this._pollTimer);
				this._pollTimer = null;
			}
		},

		pollNow() {
			this.fetchJSON(`/api/stats/summary`).then((stats) => {
				this.loadStats(stats);
			});
			this.fetchJSON(`/api/stats/live`).then((rows) => {
				this.loadLiveVisitors(rows, { isPoll: true });
			});
		},

		loadStats(stats) {
			if (!stats) return;
			this.stats = stats;
		},

		formatDuration(seconds) {
			if (seconds === undefined || seconds === null || isNaN(seconds)) return "0m 0s";
			const m = Math.floor(seconds / 60);
			const s = Math.floor(seconds % 60);
			return `${m}m ${s}s`;
		},

		// Formats a short per-visit duration in whole seconds. A real,
		// nonzero engagement under half a second (e.g. 0.4s) would round
		// down to "0s" with plain Math.round, which reads as "spent no
		// time here" even though the visit genuinely happened — shown as
		// "~1s" instead so a real but brief visit isn't indistinguishable
		// from no engagement at all.
		formatShortDuration(seconds) {
			if (seconds === undefined || seconds === null || isNaN(seconds)) return "0s";
			const rounded = Math.round(seconds);
			if (rounded === 0 && seconds > 0) return "~1s";
			return `${rounded}s`;
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


			canvas = freshCanvas(canvas);


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
					plugins: {
						legend: { display: false },
						tooltip: {
							callbacks: {
								label: (ctx) => `${ctx.parsed.y} unique visitor${ctx.parsed.y === 1 ? "" : "s"}`,
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
			this.topReferrers = this.toBreakdownRows(referrers, "referrer", "count");
		},

		loadCountries(rows) {
			if (!rows) {
				this.countries = [];
				return;
			}
			this.countries = rows.map((row) => {
				const info = this.countryInfo[row.code];
				return {
					...row,
					name: (info && info.name) || row.code,
					tier: info ? info.tier : "?",
				};
			});
		},

		loadDevices(devices) {
			if (!devices) return;
			this.devices = devices;
		},

		// Browser bucketing itself happens server-side.
		// This just passes through whatever labels arrive — if "Other" is
		// still swallowing a lot of traffic, expand the browser-detection
		// list at the source (wherever the user-agent gets parsed into a
		// name), not here; this function has no classification logic to
		// widen.
		loadBrowsers(browsers) {
			if (!browsers) return;
			this.browsers = browsers;
		},

		// OS names from the UA parser include the version (e.g. "Windows
		// 10", "Mac OS X 10.15.7", "Android 13", "iOS 16.1"), not a clean
		// short label, so this matches by substring rather than exact key
		// — same approach as _referrerMatch.
		_osMatch(name) {
			const osIcons = [
				{ match: "windows", icon: "fa-brands fa-windows", color: "#00a4ef" },
				{ match: "mac os", icon: "fa-brands fa-apple", color: "#a2aaad" },
				{ match: "macos", icon: "fa-brands fa-apple", color: "#a2aaad" },
				{ match: "ios", icon: "fa-brands fa-apple", color: "#a2aaad" },
				{ match: "android", icon: "fa-brands fa-android", color: "#a4c639" },
				{ match: "linux", icon: "fa-brands fa-linux", color: "#fbbf24" },
				{ match: "ubuntu", icon: "fa-brands fa-ubuntu", color: "#e95420" },
				{ match: "chrome os", icon: "fa-brands fa-chrome", color: "#4ade80" },
			];
			return osIcons.find((o) => name.toLowerCase().includes(o.match));
		},

		osIcon(name) {
			const hit = this._osMatch(name || "");
			return hit ? hit.icon : "fa-solid fa-desktop";
		},

		osColor(name) {
			const hit = this._osMatch(name || "");
			// Unrecognized OS still gets a plain icon, but a muted teal
			// instead of flat grey — same reasoning as referrerColor's
			// fallback.
			return hit ? hit.color : "#5aa9a3";
		},

		loadOS(os) {
			if (!os) return;
			this.os = os;
		},

		loadCustomEvents(events) {
			if (!events) return;
			this.customEvents = this.toBreakdownRows(events, "event_name", "count").map((row) => ({
				...row,
				countries: (row.countries || []).map((c) => {
					const info = this.countryInfo[c.value];
					return { ...c, name: (info && info.name) || c.value };
				}),
			}));
		},

		// On a full loadAll() (initial load, login, range change), the
		// filter/expanded-group UI state is reset since the visitor list
		// itself is starting fresh. On a background poll tick (isPoll:
		// true), that same state is deliberately left alone — otherwise
		// anyone who filtered by country or expanded a page group would
		// get silently reset back to the default view every 30 seconds.
		loadLiveVisitors(visitors, { isPoll = false } = {}) {
			if (!visitors) return;
			this.liveVisitors = visitors.map((v) => {
				const info = this.countryInfo[v.country_code];
				return { ...v, country_name: (info && info.name) || v.country_code };
			});
			if (!isPoll) {
				this.liveFilterValue = "";
				this.expandedLiveGroups = [];
			}
		},

		// Visitors matching the current filter (field + value). With no
		// field selected, or a field selected but no value yet, every
		// visitor passes through unfiltered.
		get filteredLiveVisitors() {
			if (!this.liveFilterField || !this.liveFilterValue) return this.liveVisitors;
			return this.liveVisitors.filter((v) => v[this.liveFilterField] === this.liveFilterValue);
		},

		// The distinct values available for whichever field is currently
		// selected in the filter, so the second dropdown only ever offers
		// choices that actually exist in the live data right now.
		get liveFilterOptions() {
			if (!this.liveFilterField) return [];
			const values = this.liveVisitors.map((v) => v[this.liveFilterField]);
			return Array.from(new Set(values)).sort();
		},

		// Groups the filtered visitors by page, preserving first-seen page
		// order (rather than alphabetical) so the busiest/most-recently-
		// active pages tend to surface first, matching how the rest of
		// the dashboard orders breakdown lists by relevance, not sorting.
		get liveVisitorGroups() {
			const groups = [];
			const byPage = new Map();
			for (const v of this.filteredLiveVisitors) {
				if (!byPage.has(v.page)) {
					const group = { page: v.page, visitors: [] };
					byPage.set(v.page, group);
					groups.push(group);
				}
				byPage.get(v.page).visitors.push(v);
			}
			return groups;
		},

		toggleLiveGroup(page) {
			const i = this.expandedLiveGroups.indexOf(page);
			if (i === -1) this.expandedLiveGroups.push(page);
			else this.expandedLiveGroups.splice(i, 1);
		},

		openLiveVisitorsModal() {
			this.liveModalView = "page";
			this.liveModalOpen = true;
		},

		closeLiveVisitorsModal() {
			this.liveModalOpen = false;
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
				if (tab === "mcp" && !this._mcpStatusLoaded) {
					this._mcpStatusLoaded = true;
					this.loadMcpTokenStatus();
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

		async openFunnelManager() {
			this.funnelManagerOpen = true;
			this.funnelEditing = null;
			this.funnelFormError = "";
			const [names, defs] = await Promise.all([
				this.fetchJSON("/api/event-names"),
				this.fetchJSON("/api/funnels"),
			]);
			this.availableEventNames = names || [];
			this.funnelDefs = defs || [];
		},

		closeFunnelManager() {
			this.funnelManagerOpen = false;
			this.funnelEditing = null;
		},

		startNewFunnel() {
			this.funnelFormError = "";
			this.funnelEditing = {
				id: null,
				name: "",
				window_hours: 168,
				steps: [{ event_name: "", label: "" }, { event_name: "", label: "" }],
			};
		},

		startEditFunnel(f) {
			this.funnelFormError = "";
			this.funnelEditing = {
				id: f.id,
				name: f.name,
				window_hours: f.window_hours || 168,
				steps: f.steps.map((s) => ({ event_name: s.event_name, label: s.label })),
			};
		},

		cancelEditFunnel() {
			this.funnelEditing = null;
			this.funnelFormError = "";
		},

		availableEventNamesFor(stepIndex) {
			const current = this.funnelEditing.steps[stepIndex].event_name;
			const usedElsewhere = this.funnelEditing.steps
				.filter((_, i) => i !== stepIndex)
				.map((s) => s.event_name)
				.filter(Boolean);
			return this.availableEventNames.filter((n) => n === current || !usedElsewhere.includes(n));
		},

		addFunnelStep() {
			this.funnelEditing.steps.push({ event_name: "", label: "" });
		},

		removeFunnelStep(i) {
			if (this.funnelEditing.steps.length <= 2) return;
			this.funnelEditing.steps.splice(i, 1);
		},

		async saveFunnel() {
			const f = this.funnelEditing;
			if (!f.name.trim()) {
				this.funnelFormError = "Funnel name is required.";
				return;
			}
			const steps = f.steps.filter((s) => s.event_name);
			if (steps.length < 2) {
				this.funnelFormError = "At least 2 steps with an event selected are required.";
				return;
			}

			this.funnelSaving = true;
			this.funnelFormError = "";
			try {
				const body = JSON.stringify({ name: f.name, window_hours: f.window_hours || 168, steps });
				const url = f.id ? `/api/funnels/${f.id}` : "/api/funnels";
				const method = f.id ? "PUT" : "POST";
				const res = await fetch(url, { method, headers: { "Content-Type": "application/json" }, body });
				if (!res.ok) {
					this.funnelFormError = "Failed to save funnel.";
					return;
				}
				this.funnelEditing = null;
				const [rows, defs] = await Promise.all([
					this.fetchJSON(`/api/stats/funnels?${this.dateRangeParams()}`),
					this.fetchJSON("/api/funnels"),
				]);
				this.loadFunnels(rows);
				this.funnelDefs = defs || [];
			} catch {
				this.funnelFormError = "Couldn't reach the server.";
			} finally {
				this.funnelSaving = false;
			}
		},

		async deleteFunnel(id) {
			try {
				await fetch(`/api/funnels/${id}`, { method: "DELETE" });
				const [rows, defs] = await Promise.all([
					this.fetchJSON(`/api/stats/funnels?${this.dateRangeParams()}`),
					this.fetchJSON("/api/funnels"),
				]);
				this.loadFunnels(rows || []);
				if (rows === null || rows.length === 0) this.funnels = [];
				this.funnelDefs = defs || [];
			} catch {
				// best-effort; the row simply won't disappear from the list on failure
			}
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
								label: (ctx) => {
									const p = points[ctx.dataIndex];
									if (p && typeof p.active === "number" && typeof p.cohort_size === "number") {
										return `${ctx.parsed.y}% retained (${p.active} of ${p.cohort_size} visitors)`;
									}
									return `${ctx.parsed.y}% retained`;
								},
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
				"Chrome (WebView)": "fa-brands fa-chrome",
				Safari: "fa-brands fa-safari",
				Firefox: "fa-brands fa-firefox-browser",
				Edge: "fa-brands fa-edge",
				Opera: "fa-brands fa-opera",
				Brave: "fa-brands fa-brave",
				Vivaldi: "fa-solid fa-v",
				"Samsung Internet": "fa-brands fa-android",
				Android: "fa-brands fa-android",
				"Android WebView": "fa-brands fa-android",
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
				"Chrome (WebView)": "#4ade80",
				Safari: "#60a5fa",
				Firefox: "#fb923c",
				Edge: "#22d3ee",
				Opera: "#f87171",
				Brave: "#fb7185",
				Vivaldi: "#a78bfa",
				"Samsung Internet": "#60a5fa",
				Android: "#a4c639",
				"Android WebView": "#a4c639",
				UC: "#fbbf24",
				IE: "#38bdf8",
				"Internet Explorer": "#38bdf8",
				// "Other"/unrecognized still gets a plain globe icon, but a
				// muted teal instead of flat grey so it doesn't read as a
				// broken or error state next to the branded icons.
				Other: "#5aa9a3",
			};
			return colors[name] || "#5aa9a3";
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
			// Unrecognized referrers still get the plain globe icon, but a
			// muted teal instead of flat grey so it doesn't read as broken.
			return hit ? hit.color : "#5aa9a3";
		},

		// True for brand icons rendered as a solid badge (see
		// .brand-badge in CSS) rather than a plain colored glyph.
		referrerIsBadge(row) {
			const domain = (row.referrer || row.label || "").toLowerCase();
			const hit = this._referrerMatch(domain);
			return !!(hit && hit.badge);
		},

		_referrerMatch(domain) {
			const socialIcons = [
				// t.co and x.com are both X (formerly Twitter) — t.co is X's
				// own link-shortener domain, x.com is the site itself.
				// badge: true means render a black circular badge behind a
				// white glyph (see .brand-badge in CSS) instead of a plain
				// colored icon — X's real black doesn't have contrast as a
				// plain glyph color against either theme's background.
				{ match: "t.co", icon: "fa-brands fa-x-twitter", color: "#fff", badge: true },
				{ match: "x.com", icon: "fa-brands fa-x-twitter", color: "#fff", badge: true },
				{ match: "twitter", icon: "fa-brands fa-x-twitter", color: "#fff", badge: true },
				{ match: "facebook", icon: "fa-brands fa-facebook", color: "#1877f2" },
				{ match: "instagram", icon: "fa-brands fa-instagram", color: "#e1306c" },
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

		// Flattens nested metric objects ({value, value_seconds, value_pct,
		// delta_pct, unique_events, ...}) into individual columns, e.g.
		// unique_visitors_today -> unique_visitors_today_value,
		// unique_visitors_today_delta_pct, etc. CSV has no concept of
		// nesting, so without this every metric cell in the stats section
		// just stringifies to "[object Object]". Applied for all export
		// formats (not just CSV) so JSON/JSONL/CSV all show the same shape.
		flattenStatsForExport(stats) {
			if (!stats) return {};
			const flat = {};
			for (const [metric, val] of Object.entries(stats)) {
				if (val && typeof val === "object" && !Array.isArray(val)) {
					for (const [field, v] of Object.entries(val)) {
						flat[`${metric}_${field}`] = v;
					}
				} else {
					flat[metric] = val;
				}
			}
			return flat;
		},

		// Splits each custom event's nested properties[] (property name +
		// its value breakdown) into its own flat row: one row per
		// event/property/value combination. Mirrors the funnels flatten
		// below — nested arrays-of-objects can't live in a single CSV
		// cell, so they get pulled out into a joinable child section
		// (joined back to the parent by event_name) instead of a section
		// of its own with the array crammed in as a stringified blob.
		flattenCustomEventProperties(events) {
			if (!events) return [];
			const rows = [];
			for (const evt of events) {
				for (const prop of evt.properties || []) {
					for (const b of prop.breakdown || []) {
						rows.push({
							event_name: evt.event_name,
							property_name: prop.name,
							value: b.value,
							count: b.count,
						});
					}
				}
			}
			return rows;
		},

		// Flattens every section of the raw payload into { sectionName:
		// rows[] } — rows are always arrays, even for single-object
		// sections like `stats`, so every downstream formatter
		// (toCSV/toJSONL) can treat all sections uniformly.
		buildExportSections() {
			const sections = {
				stats: this.stats ? [this.flattenStatsForExport(this.stats)] : [],
				visitors_over_time: this.visitorsOverTime?.points || [],
				traffic_sources: this.trafficSources || [],
				top_pages: this.topPages || [],
				top_referrers: this.topReferrers || [],
				countries: this.countries || [],
				devices: this.devices || [],
				browsers: this.browsers || [],
				os: this.os || [],
				// properties omitted here (nested array of objects can't
				// live in a flat CSV cell) — see custom_event_properties
				// below, joined back to this section by event_name.
				custom_events: (this.customEvents || []).map(({ properties, ...rest }) => rest),
				custom_event_properties: this.flattenCustomEventProperties(this.customEvents),
				funnels: (this.funnels || []).flatMap((f) =>
					(f.steps || []).map((s) => ({ funnel_id: f.id, funnel_name: f.name, ...s }))
				),
				retention: this.retention?.points || [],
			};

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
			return JSON.stringify(this.buildExportSections(), null, 2);
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
			if (!this.stats && this.countries.length === 0 && this.topPages.length === 0) return;

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
		//
		// The static connect URL embeds the token directly in the path
		// (/mcp/{token}/sse) for clients that can only be configured with
		// a bare URL. The plaintext token is only ever known right after
		// generate() returns it — mcpHasToken (from the status endpoint)
		// never carries the value itself, so a page reload always shows
		// "Regenerate" with no way to recover the old connect URL, only
		// issue a new one.

		mcpConnectUrl() {
			if (!this.mcpNewToken) return "";
			return `${window.location.origin}/mcp/${this.mcpNewToken}/sse`;
		},

		mcpConfigBlock() {
			const url = this.mcpNewToken
				? this.mcpConnectUrl()
				: `${window.location.origin}/mcp/sse`;
			const auth = this.mcpNewToken
				? ""
				: `,
      "headers": { "Authorization": "Bearer YOUR_TOKEN" }`;
			return `{
  "mcpServers": {
    "gnat": {
      "url": "${url}",
      "transport": "sse"${auth}
    }
  }
}`;
		},

		async loadMcpTokenStatus() {
			const status = await this.fetchJSON("/api/dashboard/mcp-token");
			this.mcpHasToken = !!(status && status.has_token);
		},

		async generateMcpToken() {
			if (
				this.mcpHasToken &&
				!confirm("Regenerating replaces the current token. Any agent using the old one will stop working. Continue?")
			) {
				return;
			}
			this.mcpTokenLoading = true;
			this.mcpGenerateError = "";
			try {
				const res = await fetch("/api/dashboard/mcp-token/generate", { method: "POST" });
				if (!res.ok) {
					this.mcpGenerateError = "Failed to generate token.";
					return;
				}
				const data = await res.json();
				this.mcpNewToken = data.token;
				this.mcpHasToken = true;
			} catch {
				this.mcpGenerateError = "Couldn't reach the server.";
			} finally {
				this.mcpTokenLoading = false;
			}
		},

		async copyMcpBlock(which) {
			const blocks = {
				config: this.mcpConfigBlock(),
				url: this.mcpConnectUrl(),
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
				os: { title: "Operating Systems", rows: this.os },
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

		// Same drill-down, but entered from inside the shared "View All"
		// modal rather than the main Overview list. Closes that modal
		// first rather than stacking a second backdrop on top of it,
		// which otherwise leaves two modal-backdrops fighting over Escape
		// and click-outside handling.
		openEventDetailFromModal(row) {
			this.closeModal();
			this.openEventDetail(row);
		},

		closeEventDetail() {
			this.eventDetailOpen = false;
		},

		// ---- country drill-down ------------------------------------------

		// Opens the country detail modal and fetches its data fresh —
		// unlike event detail (whose data already sits in customEvents),
		// this is a new query scoped by country + the current date range,
		// so there's nothing to show until the fetch resolves.
		async openCountryDetail(row) {
			const code = row.code || row.value;
			if (!code) return;

			this.countryDetail = null;
			this.countryDetailLoading = true;
			this.countryDetailOpen = true;
			this.expandedCountryPage = null;

			const data = await this.fetchJSON(
				`/api/stats/country-detail?country=${encodeURIComponent(code)}&${this.dateRangeParams()}`
			);

			// The person may have closed the modal (or opened a different
			// country) before this resolved — don't clobber whatever's
			// current with a stale response.
			if (!this.countryDetailOpen) return;

			this.countryDetail = data;
			this.countryDetailLoading = false;
		},

		// Same drill-down, but entered from inside the shared "View All"
		// modal rather than the main Overview list — see
		// openEventDetailFromModal's comment for why the outer modal
		// closes first.
		openCountryDetailFromModal(row) {
			this.closeModal();
			this.openCountryDetail(row);
		},

		closeCountryDetail() {
			this.countryDetailOpen = false;
			this.countryDetail = null;
			this.countryDetailLoading = false;
			this.expandedCountryPage = null;
		},

		// Toggles the time-on-page detail row for one page in the country
		// modal's Pages Visited list — only one open at a time, click
		// again (or click a different page) to switch/collapse.
		toggleCountryPage(path) {
			this.expandedCountryPage = this.expandedCountryPage === path ? null : path;
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

// Tap-to-open tooltips on touch devices, delegated listener outside Alpine (global, not per-instance).
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

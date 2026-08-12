(function () {
	"use strict";

	if (window.__gnatInitialized) {
		return;
	}
	window.__gnatInitialized = true;

	var scriptTag = document.currentScript;
	var siteKey = scriptTag ? scriptTag.getAttribute("data-site-key") : null;
	var endpoint = scriptTag ? scriptTag.src.replace(/\/tracker\.js.*$/, "/api/event") : "/api/event";

	if (!siteKey) {
		console.warn("gnat: no data-site-key set on tracker script tag, tracking disabled");
		return;
	}

	var INACTIVITY_THRESHOLD = 30000; // 30s idle = no longer "active"
	var currentPath = location.pathname;
	var activeTime = 0;
	var lastActivityTime = Date.now();
	var isActive = true;
	var inactivityTimer = null;
	var pageLeft = false;

	// --- identity ---

	function getDistinctId() {
		var key = "_gnat_id";
		try {
			var id = localStorage.getItem(key);
			if (!id) {
				id = generateId();
				localStorage.setItem(key, id);
			}
			return id;
		} catch (e) {
			return generateId();
		}
	}

	function generateId() {
		return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, function (c) {
			var r = (Math.random() * 16) | 0;
			var v = c === "x" ? r : (r & 0x3) | 0x8;
			return v.toString(16);
		});
	}

	var distinctId = getDistinctId();

	// --- sending ---

	// useBeacon: sendBeacon survives page unload in a way fetch is not
	// guaranteed to. Used for pageview_end/leave. Falls back to fetch with
	// keepalive if sendBeacon is unavailable or the browser rejects it.
	// Note: sendBeacon cannot carry custom headers, so for beacon sends the
	// API key travels in the JSON body instead of the X-API-Key header. The
	// server needs to accept the key from either place.
	function send(eventName, properties, useBeacon) {
		var payload = {
			event_name: eventName,
			distinct_id: distinctId,
			properties: properties || {},
			timestamp: new Date().toISOString()
		};

		if (useBeacon && navigator.sendBeacon) {
			var beaconPayload = JSON.parse(JSON.stringify(payload));
			beaconPayload.api_key = siteKey;
			var blob = new Blob([JSON.stringify(beaconPayload)], { type: "text/plain" });
			var sent = navigator.sendBeacon(endpoint, blob);
			if (sent) return;
			// fall through to fetch if the beacon was rejected
		}

		fetch(endpoint, {
			method: "POST",
			headers: {
				"Content-Type": "application/json",
				"X-API-Key": siteKey
			},
			body: JSON.stringify(payload),
			keepalive: true
		}).catch(function () {
			// silently drop network errors, analytics should never break the page
		});
	}

	// --- active time tracking ---
	// Tracks real engaged time (mouse move, scroll, click, keypress, touch),
	// not just "tab was open." A background tab does not accumulate time.

	function updateActiveTime() {
		if (isActive && document.visibilityState === "visible") {
			var now = Date.now();
			var delta = now - lastActivityTime;
			if (delta > 0 && delta < INACTIVITY_THRESHOLD) {
				activeTime += delta;
			}
			lastActivityTime = now;
		}
	}

	function markActive() {
		var now = Date.now();
		if (!isActive) {
			isActive = true;
			lastActivityTime = now;
		} else {
			updateActiveTime();
		}
		clearTimeout(inactivityTimer);
		inactivityTimer = setTimeout(markInactive, INACTIVITY_THRESHOLD);
	}

	function markInactive() {
		if (isActive) {
			updateActiveTime();
			isActive = false;
		}
	}

	// --- pageview lifecycle ---

	var HEARTBEAT_INTERVAL = 30000; // 30s, keeps live-visitor tracking fresh
	var heartbeatTimer = null;

	function sendHeartbeat() {
		if (document.visibilityState === "visible" && !pageLeft) {
			send("heartbeat", { path: currentPath });
		}
	}

	function startHeartbeat() {
		clearInterval(heartbeatTimer);
		heartbeatTimer = setInterval(sendHeartbeat, HEARTBEAT_INTERVAL);
	}

	function trackPageview() {
		send("pageview", {
			path: location.pathname,
			referrer: document.referrer || null
		});
	}

	function trackPageviewEnd() {
		updateActiveTime();
		var seconds = Math.round(activeTime / 1000);
		send("pageview_end", {
			path: currentPath,
			timespent: seconds
		}, true);
	}

	function handleRouteChange() {
		var newPath = location.pathname;
		if (newPath === currentPath) return;

		trackPageviewEnd();

		currentPath = newPath;
		activeTime = 0;
		lastActivityTime = Date.now();
		isActive = true;
		pageLeft = false;

		// let location.href settle before reading it again for the new pageview
		setTimeout(trackPageview, 0);
	}

	function handlePageLeave() {
		if (pageLeft) return;
		pageLeft = true;
		trackPageviewEnd();
	}

	// --- wire up ---

	trackPageview();
	startHeartbeat();

	var activityEvents = ["mousedown", "mousemove", "keypress", "scroll", "touchstart", "click"];
	activityEvents.forEach(function (evt) {
		document.addEventListener(evt, markActive, { passive: true });
	});

	document.addEventListener("visibilitychange", function () {
		if (document.visibilityState === "hidden") {
			markInactive();
		} else {
			markActive();
		}
	});

	window.addEventListener("beforeunload", handlePageLeave, { capture: true });
	window.addEventListener("pagehide", handlePageLeave, { passive: true });

	inactivityTimer = setTimeout(markInactive, INACTIVITY_THRESHOLD);

	// SPA route changes: patch pushState/replaceState since browsers fire
	// no native event for them, plus popstate for back/forward.
	var originalPushState = history.pushState;
	history.pushState = function () {
		originalPushState.apply(history, arguments);
		handleRouteChange();
	};

	var originalReplaceState = history.replaceState;
	history.replaceState = function () {
		originalReplaceState.apply(history, arguments);
		handleRouteChange();
	};

	window.addEventListener("popstate", handleRouteChange, { passive: true });

	// --- public API ---

	window.gnat = {
		track: function (eventName, properties) {
			if (!eventName) {
				console.warn("gnat: track() requires an event name");
				return;
			}
			send(eventName, properties);
		}
	};
})();

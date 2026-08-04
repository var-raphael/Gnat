(function () {
	"use strict";

	var scriptTag = document.currentScript;
	var siteKey = scriptTag ? scriptTag.getAttribute("data-site-key") : null;
	var endpoint = scriptTag ? scriptTag.src.replace(/\/tracker\.js.*$/, "/api/event") : "/api/event";

	if (!siteKey) {
		console.warn("gnat: no data-site-key set on tracker script tag, tracking disabled");
		return;
	}

	function send(eventName, properties) {
	var payload = {
		event_name: eventName,
		distinct_id: getDistinctId(),
		properties: properties || {},
		timestamp: new Date().toISOString()
	};

	console.log("gnat: sending event to", endpoint, payload);

	fetch(endpoint, {
		method: "POST",
		headers: {
			"Content-Type": "application/json",
			"X-API-Key": siteKey
		},
		body: JSON.stringify(payload),
		keepalive: true
	}).then(function (res) {
		console.log("gnat: response status", res.status);
	}).catch(function (err) {
		console.error("gnat: fetch failed", err);
	});
}

	// distinct_id: a simple anonymous per-browser identifier stored in
	// localStorage. No cookies, no cross-site tracking, matches the
	// privacy-first positioning.
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
			// localStorage unavailable (private browsing etc), fall back to
			// a per-load id, still better than nothing
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

	function trackPageview() {
		send("pageview", {
			path: location.pathname,
			referrer: document.referrer || null
		});
	}

	// --- automatic pageview tracking, including SPA route changes ---

	trackPageview();

	// SPA frameworks (React Router, Next.js, Vue Router, etc.) navigate via
	// history.pushState without a full page load, so a plain "load" listener
	// only ever fires once. Patch pushState/replaceState to also notify us.
	var originalPushState = history.pushState;
	history.pushState = function () {
		originalPushState.apply(history, arguments);
		trackPageview();
	};

	var originalReplaceState = history.replaceState;
	history.replaceState = function () {
		originalReplaceState.apply(history, arguments);
		trackPageview();
	};

	window.addEventListener("popstate", trackPageview);

	// --- public API for custom events ---

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

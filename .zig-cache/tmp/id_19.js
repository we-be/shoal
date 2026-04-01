new Promise(resolve => {
		let timer;
		const reset = () => {
			clearTimeout(timer);
			timer = setTimeout(resolve, 500);
		};
		const orig = window.fetch;
		window.fetch = function() { reset(); return orig.apply(this, arguments); };
		const origXHR = XMLHttpRequest.prototype.send;
		XMLHttpRequest.prototype.send = function() { reset(); return origXHR.apply(this, arguments); };
		reset();
	})
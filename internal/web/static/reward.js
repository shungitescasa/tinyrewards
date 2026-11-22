const filter = document.querySelector('[data-emote-filter]');
const grid = document.querySelector('[data-emotes]');

if (filter && grid) {
	const items = [...grid.querySelectorAll('[data-name]')].map(el => ({
		el,
		provider: el.dataset.provider,
		name: el.dataset.name.toLowerCase(),
	}));
	const empty = document.querySelector('[data-emotes-empty]');
	const search = filter.elements.q;
	filter.querySelector('button[type="submit"]').hidden = true;

	const apply = () => {
		const provider = filter.elements.provider.value;
		const query = search.value.trim().toLowerCase();
		let shown = 0;
		for (const item of items) {
			const match = (!provider || item.provider === provider) && item.name.includes(query);
			item.el.hidden = !match;
			if (match) shown++;
		}
		empty.hidden = shown > 0;
		for (const radio of filter.elements.provider) {
			radio.parentElement.classList.toggle('active', radio.checked);
		}
	};

	filter.addEventListener('input', apply);
	filter.addEventListener('submit', event => {
		event.preventDefault();
		apply();
	});
	apply();
}

for (const group of document.querySelectorAll('[data-tabs]')) {
	const sections = [...group.querySelectorAll(':scope > .box[id]')];
	if (sections.length < 2) continue;

	const box = document.createElement('section');
	box.className = 'box';
	const tabs = document.createElement('nav');
	tabs.className = 'tabs';
	box.append(tabs);

	const panels = sections.map(section => {
		const id = section.id;
		const link = document.createElement('a');
		link.href = '#' + id;
		link.textContent = section.querySelector(':scope > .tab').textContent;
		tabs.append(link);

		const panel = section.querySelector(':scope > .content');
		section.removeAttribute('id');
		panel.id = id;
		box.append(panel);
		section.remove();
		return { link, panel };
	});
	group.prepend(box);

	const select = id => {
		const current = panels.find(p => p.panel.id === id) || panels[0];
		for (const p of panels) {
			p.panel.hidden = p !== current;
			p.link.classList.toggle('active', p === current);
		}
	};

	tabs.addEventListener('click', event => {
		const link = event.target.closest('a');
		if (!link) return;
		event.preventDefault();
		history.replaceState(null, '', link.hash);
		select(link.hash.slice(1));
	});

	select(location.hash.slice(1));
}

(async () => {
  const B = window.BOOT, T = B.t;
  const panel = document.getElementById('panel');
  const css = (n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim();
  const yen = (v) => '¥' + Math.round(v).toLocaleString();
  const pct = (d) => (d > 0 ? '+' : '') + (d * 100).toFixed(1) + '%';
  let metric = 'retail';
  const muniGeo = {};          // pref code → GeoJSON
  let shownPref = null;

  const BASEMAP = 'https://tiles.openfreemap.org/styles/positron';
  const EMPTY = { version: 8, sources: {}, layers: [{ id: 'bg', type: 'background', paint: { 'background-color': css('--page') } }] };

  // The basemap is optional: if its style can't be fetched in time, use a plain background.
  const style = await Promise.race([
    fetch(BASEMAP).then((r) => (r.ok ? r.json() : EMPTY)).catch(() => EMPTY),
    new Promise((ok) => setTimeout(() => ok(EMPTY), 4000)),
  ]);
  const prefGeo = await (await fetch('/geo/pref.json')).json(); // kept for bounds and names
  const JAPAN = [[122.5, 24], [146, 45.6]];
  const map = new maplibregl.Map({
    container: 'map', style, bounds: JAPAN, fitBoundsOptions: { padding: panelPadding() },
    minZoom: 3.5, maxZoom: 12, maxBounds: [[110, 12], [170, 54]],
    attributionControl: { compact: true },
  });
  map.addControl(new maplibregl.NavigationControl({ showCompass: false }), 'bottom-right');

  // Drawn through the straits so Korea, Russia, China and Taiwan fall outside while every
  // Japanese island (Tsushima, Yonaguni, Okinotorishima, Minamitorishima, Kurils) stays in.
  const JAPAN_WATERS = [[140.8, 45.65], [142.3, 45.5], [145.5, 44.9], [147.8, 45.6], [149.2, 45.75], [150.5, 45],
    [155, 40], [155.5, 24.5], [154.5, 23.5], [137, 19.6], [135.2, 20.3], [124.5, 20.5], [122.6, 23.6],
    [122.6, 24.8], [123.2, 26.2], [125.8, 28.8], [127.6, 31.6], [128.6, 33.6], [128.95, 34.3], [129.3, 34.95],
    [130.6, 35.7], [131.4, 37.6], [134.5, 40.5], [137.8, 43.5], [139.6, 45.7], [140.8, 45.65]];

  const fillColor = () => {
    const c = ['--c-2', '--c-1', '--c0', '--c1', '--c2'].map(css);
    return ['case', ['boolean', ['feature-state', 'has'], false],
      ['match', ['feature-state', 'c'], -2, c[0], -1, c[1], 0, c[2], 1, c[3], 2, c[4], c[2]],
      'rgba(150,150,140,0.18)'];
  };

  // A style object can finish loading before a listener is attached; cover both orders.
  let started = false;
  const start = async () => {
    if (started) return;
    started = true;
    map.addSource('pref', { type: 'geojson', data: prefGeo, promoteId: 'code' });
    map.addSource('muni', { type: 'geojson', data: { type: 'FeatureCollection', features: [] }, promoteId: 'code' });
    const layers = map.getStyle().layers;
    const before = layers.find((l) => l.type === 'symbol')?.id; // keep place labels on top
    // Place names in the page language, falling back to Japanese, then the local name.
    for (const l of layers) {
      if (l.type === 'symbol' && l.layout && l.layout['text-field']) {
        map.setLayoutProperty(l.id, 'text-field', ['coalesce', ['get', `name:${B.lang}`], ['get', 'name:ja'], ['get', 'name']]);
      }
    }
    map.addLayer({ id: 'pref-fill', type: 'fill', source: 'pref', paint: { 'fill-color': fillColor(), 'fill-opacity': 0.82 } }, before);
    map.addLayer({ id: 'pref-line', type: 'line', source: 'pref', paint: {
      'line-color': ['case', ['boolean', ['feature-state', 'hover'], false], css('--ink'), css('--card')],
      'line-width': ['case', ['boolean', ['feature-state', 'hover'], false], 2, 0.8] } }, before);
    map.addLayer({ id: 'muni-fill', type: 'fill', source: 'muni', paint: { 'fill-color': fillColor(), 'fill-opacity': 0.88 } }, before);
    map.addLayer({ id: 'muni-line', type: 'line', source: 'muni', paint: {
      'line-color': ['case', ['boolean', ['feature-state', 'hover'], false], css('--ink'), css('--card')],
      'line-width': ['case', ['boolean', ['feature-state', 'hover'], false], 2, 0.6] } }, before);
    // Hide other countries: everything outside Japan's waters is painted over in the sea color.
    const water = layers.find((l) => l.id === 'water')?.paint?.['fill-color'];
    map.addSource('mask', { type: 'geojson', data: { type: 'Polygon', coordinates: [
      [[-180, -85], [180, -85], [180, 85], [-180, 85], [-180, -85]], JAPAN_WATERS] } });
    map.addLayer({ id: 'mask', type: 'fill', source: 'mask',
      paint: { 'fill-color': typeof water === 'string' ? water : css('--page'), 'fill-antialias': false } });
    await paintPref();
    const el = panel.firstElementChild;
    if (B.kind === 'pref') await showPref(B.code, true);
    if (B.kind === 'muni') await showMuni(B.code, el && el.dataset.pref);
  };
  map.once('style.load', start);
  if (map.isStyleLoaded()) start();

  async function paint(source, url) {
    const r = await (await fetch(url)).json();
    map.removeFeatureState({ source });
    for (const [code, v] of Object.entries(r.values)) {
      map.setFeatureState({ source, id: code }, { has: true, c: v.c, v: v.v, d: v.d, n: v.n || 0 });
    }
    document.getElementById('map-label').textContent = r.label || T.no_data;
    return r;
  }

  const prefMetric = () => (metric === 'store' ? 'retail' : metric);
  const paintPref = () => paint('pref', `/api/map?level=pref&metric=${prefMetric()}`);
  const paintMuni = (pref) => paint('muni', `/api/map?level=muni&pref=${pref}&metric=${metric === 'store' ? 'store' : 'retail'}`);

  // Bounds of the largest part, so remote islands (Tokyo's Ogasawara) don't zoom us out to sea.
  function bounds(geom) {
    const parts = geom.type === 'MultiPolygon' ? geom.coordinates : [geom.coordinates];
    let best = null, bestArea = -1;
    for (const p of parts) {
      const b = new maplibregl.LngLatBounds();
      p[0].forEach((c) => b.extend(c));
      const area = (b.getEast() - b.getWest()) * (b.getNorth() - b.getSouth());
      if (area > bestArea) { best = b; bestArea = area; }
    }
    return best;
  }

  async function showPref(code, fit) {
    if (shownPref !== code) {
      if (!muniGeo[code]) muniGeo[code] = await (await fetch(`/geo/muni/${code}.json`)).json();
      map.getSource('muni').setData(muniGeo[code]);
      shownPref = code;
    }
    await paintMuni(code);
    const f = prefGeo.features.find((x) => x.properties.code === code);
    if (fit && f) map.fitBounds(bounds(f.geometry), { padding: panelPadding(), maxZoom: 10 });
  }

  async function showMuni(code, pref) {
    await showPref(pref || code.slice(0, 2), false);
    const f = muniGeo[pref || code.slice(0, 2)].features.find((x) => x.properties.code === code);
    if (f) map.fitBounds(bounds(f.geometry), { padding: panelPadding(), maxZoom: 12 });
  }

  function clearMuni() {
    map.getSource('muni').setData({ type: 'FeatureCollection', features: [] });
    shownPref = null;
    map.fitBounds(JAPAN, { padding: panelPadding() });
  }

  function panelPadding() {
    if (innerWidth <= 720) return { top: 120, bottom: innerHeight * 0.48, left: 20, right: 20 };
    return { top: 70, bottom: 50, left: 390, right: 240 };
  }

  // Panel navigation: same URLs the server renders, swapped in place.
  async function go(kind, code, push = true) {
    const r = await fetch(`/api/panel?lang=${B.lang}&kind=${kind}&code=${code || ''}`);
    if (!r.ok) return;
    panel.innerHTML = await r.text();
    panel.scrollTop = 0;
    const path = kind === 'home' ? `/${B.lang}/` : `/${B.lang}/${kind}/${code}`;
    if (push && path !== location.pathname) history.pushState({ kind, code }, '', path);
    document.querySelector('.lang').href = path.replace(`/${B.lang}/`, `/${B.lang === 'ko' ? 'ja' : 'ko'}/`);
    if (kind === 'home') clearMuni();
    if (kind === 'pref') await showPref(code, true);
    if (kind === 'muni') await showMuni(code, panel.firstElementChild.dataset.pref);
  }

  panel.addEventListener('click', (e) => {
    const a = e.target.closest('a[data-kind]');
    if (!a || e.metaKey || e.ctrlKey) return;
    e.preventDefault();
    go(a.dataset.kind, a.dataset.code);
  });
  addEventListener('popstate', (e) => {
    const s = e.state || { kind: 'home' };
    go(s.kind, s.code, false);
  });

  // Hover card + click to drill down.
  const popup = new maplibregl.Popup({ closeButton: false, closeOnClick: false, offset: 12 });
  let hovered = null;
  function hover(layer, source) {
    map.on('mousemove', layer, (e) => {
      const f = e.features[0];
      if (hovered) map.setFeatureState(hovered, { hover: false });
      hovered = { source, id: f.properties.code };
      map.setFeatureState(hovered, { hover: true });
      map.getCanvas().style.cursor = 'pointer';
      const s = map.getFeatureState(hovered);
      const name = source === 'pref' ? nameOf(f.properties.code) : f.properties.name;
      let body = `<div>${name}</div>`;
      if (s.has) {
        body += `<b>${yen(s.v)}</b>`;
        if (source === 'muni' && metric === 'store') body += ` <span class="muted">${T.store_count.replace('%d', s.n)}</span>`;
        else body += ` <span class="muted">${metric === 'mom' ? T.vs_last_month : T.vs_national} ${pct(s.d)}</span>`;
      } else body += `<span class="muted">${T.no_data}</span>`;
      popup.setLngLat(e.lngLat).setHTML(`<div class="pop">${body}</div>`).addTo(map);
    });
    map.on('mouseleave', layer, () => {
      if (hovered) map.setFeatureState(hovered, { hover: false });
      hovered = null;
      map.getCanvas().style.cursor = '';
      popup.remove();
    });
    map.on('click', layer, (e) => {
      const code = e.features[0].properties.code;
      if (source === 'pref' && shownPref === code) return; // let the muni layer take it
      go(source, code);
    });
  }
  const nameOf = (code) => {
    const e = B.search.find((x) => x.k === 'pref' && x.c === code && (B.lang === 'ko' ? /[가-힣]/.test(x.n) : !/[가-힣]/.test(x.n)));
    return e ? e.n : code;
  };
  hover('muni-fill', 'muni');
  hover('pref-fill', 'pref');

  // Metric switch repaints whatever is shown.
  const scaleMid = document.getElementById('scale-mid');
  document.getElementById('metric').addEventListener('change', async (e) => {
    metric = e.target.value;
    scaleMid.textContent = metric === 'mom' ? '±0%' : '';
    await paintPref();
    if (shownPref) await paintMuni(shownPref);
  });

  // Search: exact name from the datalist jumps there.
  const input = document.getElementById('search');
  const list = document.getElementById('search-list');
  const seen = new Set();
  for (const e of B.search) {
    if (seen.has(e.n)) continue;
    seen.add(e.n);
    const o = document.createElement('option');
    o.value = e.n;
    list.appendChild(o);
  }
  input.addEventListener('change', () => {
    const e = B.search.find((x) => x.n === input.value.trim());
    if (e) { input.value = ''; input.blur(); go(e.k, e.c); }
  });
})();

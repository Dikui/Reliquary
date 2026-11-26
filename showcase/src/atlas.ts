/*! Natural Earth country boundaries, redistributed by https://github.com/topojson/world-atlas

Copyright 2013-2019 Michael Bostock

Permission to use, copy, modify, and/or distribute this software for any purpose
with or without fee is hereby granted, provided that the above copyright notice
and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY AND
FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM LOSS
OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER
TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF
THIS SOFTWARE.
*/
import * as d3 from 'd3-geo';
import * as topojson from 'topojson-client';
import type { Feature, MultiPolygon, Polygon } from 'geojson';
import type { GeometryCollection, Topology, Transform } from 'topojson-specification';
import atlas from 'world-atlas/countries-50m.json';
import atlasCountryCodes from './config/atlasCountryCodes.json';
import atlasSmallAreas from './config/atlasSmallAreas.json';
import { countryStandard, marketShortcuts } from './config/countryRegions';

interface CountryName {
  code: string;
  name: string;
}

interface Country extends CountryName {
  label: string;
  center: [number, number];
  small: boolean;
}

type CountryShape = Feature<Polygon | MultiPolygon, Country>;
type LandShape = Feature<Polygon | MultiPolygon, CountryName>;
type Camera = { yaw: number; pitch: number; zoom: number };
type Size = {width: number; height: number; pixelRatio: number};
type DrawOptions = Size & {
  x: number;
  y: number;
  radius: number;
  camera: Camera;
  selectedCountry?: CountryShape;
  hoveredCountry?: CountryShape;
  detail?: number;
  clip?: {x: number; y: number; width: number; height: number};
  moving?: boolean;
};

const topology = atlas as Topology<{countries: GeometryCollection<{name: string}>; land: GeometryCollection}>
  & {transform: Transform};
const countryCodes: Record<string, string> = atlasCountryCodes;
const smallAreas = atlasSmallAreas as LandShape[];
const displayNames = new Intl.DisplayNames(['zh-CN'], {type: 'region'});
const countryShapes = new Map<string, LandShape>();

function polygonParts(geometry: Polygon | MultiPolygon): MultiPolygon['coordinates'] {
  return geometry.type === 'Polygon' ? [geometry.coordinates] : geometry.coordinates;
}

const atlasShapes = topojson.feature(topology, topology.objects.countries).features
  .map((feature): LandShape => ({
    ...feature,
    geometry: feature.geometry as Polygon | MultiPolygon,
    properties: {name: feature.properties.name, code: countryCodes[feature.properties.name]},
  }));

for (const feature of [...atlasShapes, ...smallAreas]) {
  const code = feature.properties.code;
  const previous = countryShapes.get(code);
  if (!previous) countryShapes.set(code, feature);
  else previous.geometry = {
    type: 'MultiPolygon',
    coordinates: [...polygonParts(previous.geometry), ...polygonParts(feature.geometry)],
  };
}

const countries = Array.from(countryShapes.values(), (feature): CountryShape => {
  const {code, name} = feature.properties;
  const label = marketShortcuts.find((country) => country.code === code)?.label
    ?? (/^[A-Z]{2}$/.test(code) ? displayNames.of(code) : name)
    ?? name;
  // Center the camera on the largest polygon, rather than between distant territories.
  const polygon = feature.geometry.type === 'MultiPolygon'
    ? feature.geometry.coordinates.map((coordinates): Polygon => ({type: 'Polygon', coordinates}))
      .reduce((largest, part) => d3.geoArea(part) > d3.geoArea(largest) ? part : largest)
    : feature.geometry;
  return {...feature, properties: {code, name, label, center: d3.geoCentroid(polygon), small: d3.geoArea(feature) < .00008}};
});
export const globeCountries = countries.map((country) => country.properties);
const countriesByCode = new Map(countries.map((country) => [country.properties.code, country]));
const countryBounds = countries.map((feature) => ({feature, bounds: d3.geoBounds(feature)}));
const globeLand = topojson.feature(topology, topology.objects.land);
const countryBorders = topojson.mesh(topology, topology.objects.countries, (a, b) => a !== b);
// Simplify in unit-sphere coordinates so the tolerance also bounds projected error near the poles.
function simplifyArc(arc: number[][], tolerance: number) {
  if (arc.length < 3) return arc;
  const radians = Math.PI / 180;
  const points = arc.map(([longitude, latitude]) => {
    const lon = longitude * radians, lat = latitude * radians;
    const cosLat = Math.cos(lat);
    return [cosLat * Math.cos(lon), cosLat * Math.sin(lon), Math.sin(lat)];
  });
  const simplified = [arc[0]];
  const toleranceSquared = (tolerance * radians) ** 2;
  function furthestPoint(start: number, end: number) {
    const [x, y, z] = points[start];
    const [endX, endY, endZ] = points[end];
    const dx = endX - x, dy = endY - y, dz = endZ - z;
    const lengthSquared = dx * dx + dy * dy + dz * dz;
    let furthest = start;
    let maxError = 0;
    for (let i = start + 1; i < end; i++) {
      const [px, py, pz] = points[i];
      const ax = px - x, ay = py - y, az = pz - z;
      const t = lengthSquared ? Math.max(0, Math.min(1, (ax * dx + ay * dy + az * dz) / lengthSquared)) : 0;
      const error = (ax - t * dx) ** 2 + (ay - t * dy) ** 2 + (az - t * dz) ** 2;
      if (error > maxError) {
        maxError = error;
        furthest = i;
      }
    }
    return {index: furthest, error: maxError};
  }
  function appendSegment(start: number, end: number) {
    const {index, error} = furthestPoint(start, end);
    if (error <= toleranceSquared) simplified.push(arc[end]);
    else {
      appendSegment(start, index);
      appendSegment(index, end);
    }
  }
  const last = arc.length - 1;
  appendSegment(0, last);
  const closed = arc[0][0] === arc[last][0] && arc[0][1] === arc[last][1];
  if (!closed) return simplified;
  if (simplified.length < 4) {
    // Keep a closed ring for islands and holes smaller than the tolerance.
    const {index: opposite} = furthestPoint(0, last);
    const before = furthestPoint(0, opposite), after = furthestPoint(opposite, last);
    if (!before.error && !after.error) return arc;
    simplified.splice(1, simplified.length - 2, ...(before.error > after.error
      ? [arc[before.index], arc[opposite]]
      : [arc[opposite], arc[after.index]]));
  }
  const isClockwise = (ring: number[][]) => d3.geoArea({type: 'Polygon', coordinates: [ring]}) < 2 * Math.PI;
  return isClockwise(simplified) === isClockwise(arc) ? simplified : arc;
}

function simplifiedGeography(tolerance: number) {
  // Simplify each shared arc once and keep both endpoints, including on the land/country seam.
  const decode = topojson.transform(topology.transform);
  const arcs = topology.arcs.map((arc) => simplifyArc(arc.map((point, index) => decode(point, index !== 0)), tolerance));
  const simplified = {...topology, transform: undefined, arcs};
  const areas = smallAreas.map((area): LandShape => ({
    ...area,
    geometry: {type: 'MultiPolygon', coordinates: polygonParts(area.geometry).map((polygon) =>
      polygon.map((ring) => simplifyArc(ring, tolerance / 20)))},
  }));
  return {
    land: topojson.feature(simplified, simplified.objects.land),
    borders: topojson.mesh(simplified, simplified.objects.countries, (a, b) => a !== b),
    areas,
  };
}
const fullGeography = {land: globeLand, borders: countryBorders, areas: smallAreas};
const worldMotionGeography = simplifiedGeography(.36);
const zoomedMotionGeography = simplifiedGeography(.08);
const globeRadius = (width: number, height: number) => Math.min(width, height) * .437;

export function createGlobePainter(context: CanvasRenderingContext2D) {
  const projection = d3.geoOrthographic().clipAngle(90);
  const path = d3.geoPath(projection, context);
  const grid = d3.geoGraticule().step([20, 20])();
  function paint(shape: d3.GeoPermissibleObjects, fill: string | CanvasGradient | null, stroke?: string, lineWidth = 1) {
    context.beginPath();
    path(shape);
    if (fill) {
      context.fillStyle = fill;
      context.fill();
    }
    if (stroke) {
      context.strokeStyle = stroke;
      context.lineWidth = lineWidth;
      context.stroke();
    }
  }
  function draw({width, height, pixelRatio, x, y, radius, camera, selectedCountry, hoveredCountry, detail = 1, clip, moving = false}: DrawOptions) {
    const zoomedMotion = moving && camera.zoom >= 2;
    const geography = moving ? (zoomedMotion ? zoomedMotionGeography : worldMotionGeography) : fullGeography;
    projection.translate([x, y]).scale(radius).rotate([camera.yaw, camera.pitch, 0]).precision(moving ? 1.6 : .65);
    context.setTransform(pixelRatio, 0, 0, pixelRatio, 0, 0);
    context.clearRect(0, 0, width, height);
    context.save();
    if (clip) {
      context.beginPath();
      context.roundRect(clip.x, clip.y, clip.width, clip.height, 14);
      context.clip();
    }

    // Keep the lighting fixed as the Earth's surface rotates.
    const glow = context.createRadialGradient(x, y, radius * .90, x, y, radius * 1.22);
    glow.addColorStop(0, "#267fff");
    glow.addColorStop(.43, "#2660e03c");
    glow.addColorStop(1, "#2660e000");
    context.fillStyle = glow;
    context.fillRect(0, 0, width, height);

    const water = context.createRadialGradient(x - radius * .33, y - radius * .39, 0, x, y, radius);
    water.addColorStop(0, "#153773");
    water.addColorStop(.76, "#0c2254");
    water.addColorStop(1, "#061438");
    const sphere = {type: "Sphere"} as const;
    paint(sphere, water);
    paint(grid, null, "#69a9ff22", .7);

    const land = context.createLinearGradient(x - radius, y - radius, x + radius, y + radius);
    land.addColorStop(0, "#8bb8ff");
    land.addColorStop(.57, "#497cd0");
    land.addColorStop(1, "#284b91");
    context.globalAlpha = .48 + .52 * detail;
    paint(geography.land, land, "#85b4ffb0", .65);
    for (const area of geography.areas) paint(area, land, "#85b4ffb0", .65);
    if (detail > 0) {
      context.globalAlpha = detail;
      paint(geography.borders, null, "#97bfff8c", .65);
    }
    context.globalAlpha = 1;
    if (moving && (hoveredCountry || selectedCountry)) projection.precision(.65);
    if (hoveredCountry) paint(hoveredCountry, "#498aff", "#9acaff", 1.15);
    if (selectedCountry) {
      paint(selectedCountry, "#60b5ff", "#e9f4ff", 1.25);
      const center = selectedCountry.properties.center;
      if (selectedCountry.properties.small && d3.geoDistance(center, [-camera.yaw, -camera.pitch]) < Math.PI / 2) {
        const [markerX, markerY] = projection(center)!;
        context.beginPath();
        context.arc(markerX, markerY, 3, 0, Math.PI * 2);
        context.fillStyle = "#e5f0ff";
        context.fill();
      }
    }
    paint(sphere, null, "#70adffb3", 1);
    context.restore();
  }
  return {draw, projection};
}

function mountGlobe(canvas: HTMLCanvasElement, context: CanvasRenderingContext2D, onSelect: (code: string) => void) {
  const painter = createGlobePainter(context);
  const stage = canvas.parentElement!;
  const hit = stage.querySelector<HTMLElement>(".globe-hit")!;
  const label = stage.querySelector<HTMLElement>("#globe-label")!;
  const controls = stage.querySelector<HTMLElement>(".globe-tools")!;
  const buttons = {
    in: stage.querySelector<HTMLButtonElement>('[data-globe-action="in"]')!,
    out: stage.querySelector<HTMLButtonElement>('[data-globe-action="out"]')!
  };
  const initial = {yaw: -105, pitch: -23, zoom: 1};
  let camera = {...initial};
  let size = {width: 0, height: 0, pixelRatio: 1};
  let selectedCountry: CountryShape | undefined;
  let hoveredCountry: CountryShape | undefined;
  let frame = 0;
  let hoverFrame = 0;
  let settleTimer = 0;
  let labelTimer = 0;
  let labelSize = {width: 0, height: 0};
  let gestureStartZoom: number | undefined;
  type Point = {x: number; y: number};
  const pointers = new Map<number, Point & {origin: Point}>();
  let dragged = false;

  const radius = () => globeRadius(size.width, size.height) * camera.zoom;
  function draw() {
    frame = 0;
    const {width, height} = size;
    if (!width || !height) return;
    painter.draw({...size, x: width / 2, y: height / 2, radius: radius(), camera, selectedCountry, hoveredCountry,
      moving: settleTimer !== 0});
  }
  function schedule() {
    if (!frame) frame = requestAnimationFrame(draw);
  }
  function scheduleMotion() {
    clearTimeout(settleTimer);
    settleTimer = window.setTimeout(() => {
      settleTimer = 0;
      schedule();
    }, 160);
    schedule();
  }
  function setHoveredCountry(country?: CountryShape) {
    const hover = country === selectedCountry ? undefined : country;
    if (hoveredCountry === hover) return;
    hoveredCountry = hover;
    schedule();
  }
  function clearHover() {
    cancelAnimationFrame(hoverFrame);
    hoverFrame = 0;
    setHoveredCountry();
    clearLabel();
  }
  function clearLabel() {
    clearTimeout(labelTimer);
    labelTimer = 0;
    if (label.hidden) return;
    label.hidden = true;
    label.textContent = "";
    label.removeAttribute("data-selection");
  }
  function showLabel(country: CountryShape, [x, y]: [number, number], selection = false) {
    const {code, label: name} = country.properties;
    const standard = countryStandard(code);
    const text = selection && standard ? `${name} · ${standard}` : name;
    if (label.textContent !== text || label.hasAttribute("data-selection") !== selection || label.hidden) {
      label.textContent = text;
      label.toggleAttribute("data-selection", selection);
      label.hidden = false;
      labelSize = {width: label.offsetWidth, height: label.offsetHeight};
    }
    const margin = 8;
    const rightEdge = size.width - margin - labelSize.width;
    const bottomEdge = size.height - margin - labelSize.height;
    let left = x + 12 <= rightEdge ? x + 12 : x - 12 - labelSize.width;
    const top = y - margin - labelSize.height >= margin ? y - margin - labelSize.height : y + 12;
    if (top + labelSize.height > controls.offsetTop && left + labelSize.width > controls.offsetLeft) {
      left = x - 12 - labelSize.width;
    }
    label.style.left = `${Math.max(margin, Math.min(rightEdge, left))}px`;
    label.style.top = `${Math.max(margin, Math.min(bottomEdge, top))}px`;
    if (selection) {
      clearTimeout(labelTimer);
      labelTimer = window.setTimeout(clearLabel, 2100);
    }
  }
  function resizeHitArea() {
    hit.style.setProperty("--globe-hit-radius", `${radius()}px`);
  }
  function resize() {
    const bounds = canvas.getBoundingClientRect();
    const pixelRatio = Math.min(devicePixelRatio, 2);
    if (size.width === bounds.width && size.height === bounds.height && size.pixelRatio === pixelRatio) return;
    clearHover();
    size = {width: bounds.width, height: bounds.height, pixelRatio};
    canvas.width = Math.round(size.width * size.pixelRatio);
    canvas.height = Math.round(size.height * size.pixelRatio);
    resizeHitArea();
    schedule();
  }
  function setZoom(zoom: number) {
    clearHover();
    camera.zoom = Math.min(7, Math.max(1, zoom));
    buttons.in.disabled = camera.zoom >= 7;
    buttons.out.disabled = camera.zoom <= 1;
    resizeHitArea();
  }
  function rotate(yaw: number, pitch: number) {
    clearHover();
    camera.yaw = ((yaw % 360) + 540) % 360 - 180;
    camera.pitch = Math.min(90, Math.max(-90, pitch));
  }
  function resetView() {
    rotate(initial.yaw, initial.pitch);
    setZoom(initial.zoom);
  }
  function finishDraw() {
    if (frame) {
      cancelAnimationFrame(frame);
      draw();
    }
  }
  function countryAt(clientX: number, clientY: number) {
    const bounds = canvas.getBoundingClientRect();
    const x = clientX - bounds.left;
    const y = clientY - bounds.top;
    if (x < 0 || y < 0 || x >= size.width || y >= size.height ||
      Math.hypot(x - size.width / 2, y - size.height / 2) >= radius()) return;
    finishDraw();
    const point: [number, number] = [x, y];
    const coordinates = painter.projection.invert?.(point);
    if (!coordinates) return;
    const [longitude, latitude] = coordinates;
    // Check geographic bounds before testing the country shapes, including dateline crossings.
    const match = countryBounds.find(({feature, bounds: [[west, south], [east, north]]}) =>
      latitude >= south && latitude <= north &&
      (west <= east ? longitude >= west && longitude <= east : longitude >= west || longitude <= east) &&
      d3.geoContains(feature, coordinates));
    if (match) return {country: match.feature, point};
  }
  const distance = (a: Point, b: Point) => Math.hypot(a.x - b.x, a.y - b.y);
  hit.addEventListener("pointerdown", (event) => {
    if (event.button !== 0 || pointers.size >= 2) return;
    clearHover();
    canvas.focus({preventScroll: true});
    hit.setPointerCapture(event.pointerId);
    const point = {x: event.clientX, y: event.clientY};
    pointers.set(event.pointerId, {...point, origin: point});
    dragged = pointers.size > 1;
  });
  hit.addEventListener("pointermove", (event) => {
    const previous = pointers.get(event.pointerId);
    if (!previous) {
      if (pointers.size || event.pointerType === "touch" || gestureStartZoom !== undefined) return;
      cancelAnimationFrame(hoverFrame);
      hoverFrame = requestAnimationFrame(() => {
        hoverFrame = 0;
        const match = countryAt(event.clientX, event.clientY);
        if (!match) {
          clearHover();
          return;
        }
        setHoveredCountry(match.country);
        if (labelTimer && match.country === selectedCountry) return;
        if (labelTimer) clearLabel();
        showLabel(match.country, match.point);
      });
      return;
    }
    const point = {x: event.clientX, y: event.clientY};
    if (!dragged && distance(point, previous.origin) < 4) return;
    dragged = true;
    hit.classList.add("is-dragging");
    if (pointers.size === 2) {
      const other = [...pointers.entries()].find(([id]) => id !== event.pointerId)![1];
      const baseline = distance(previous, other);
      if (baseline > 0) setZoom(camera.zoom * distance(point, other) / baseline);
    } else {
      const delta = .32 / Math.sqrt(camera.zoom);
      rotate(camera.yaw + (point.x - previous.x) * delta, camera.pitch - (point.y - previous.y) * delta);
    }
    pointers.set(event.pointerId, {...point, origin: previous.origin});
    scheduleMotion();
  });
  function endPointer(event: PointerEvent) {
    if (!pointers.delete(event.pointerId)) return;
    if (!pointers.size) {
      hit.classList.remove("is-dragging");
      if (!dragged && event.type === "pointerup") {
        const match = countryAt(event.clientX, event.clientY);
        if (match) {
          onSelect(match.country.properties.code);
          showLabel(match.country, match.point, true);
        }
      }
    }
  }
  hit.addEventListener("pointerup", endPointer);
  hit.addEventListener("pointercancel", endPointer);
  hit.addEventListener("lostpointercapture", endPointer);
  hit.addEventListener("pointerleave", (event) => {
    if (event.pointerType !== "touch") clearHover();
  });
  hit.addEventListener("wheel", (event) => {
    if (!event.ctrlKey) return;
    event.preventDefault();
    if (gestureStartZoom !== undefined) return;
    const factor = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? size.height : 1;
    setZoom(camera.zoom * Math.exp(-event.deltaY * factor * .0015));
    scheduleMotion();
  }, {passive: false});
  // Safari delivers trackpad pinches as gesture events.
  hit.addEventListener("gesturestart", (event) => {
    if (pointers.size) return;
    event.preventDefault();
    gestureStartZoom = camera.zoom;
    clearHover();
  }, {passive: false});
  hit.addEventListener("gesturechange", (event) => {
    if (gestureStartZoom === undefined) return;
    event.preventDefault();
    setZoom(gestureStartZoom * (event as Event & {scale: number}).scale);
    scheduleMotion();
  }, {passive: false});
  hit.addEventListener("gestureend", () => { gestureStartZoom = undefined; });
  const keyboardRotation: Record<string, [number, number]> = {
    ArrowLeft: [-9, 0], ArrowRight: [9, 0], ArrowUp: [0, 9], ArrowDown: [0, -9]
  };
  canvas.addEventListener("keydown", (event) => {
    const delta = keyboardRotation[event.key];
    if (delta) rotate(camera.yaw + delta[0], camera.pitch + delta[1]);
    else if (event.key === "+" || event.key === "=") setZoom(camera.zoom * 1.32);
    else if (event.key === "-") setZoom(camera.zoom / 1.32);
    else if (event.key === "Home") resetView();
    else return;
    event.preventDefault();
    scheduleMotion();
  });
  controls.addEventListener("click", (event) => {
    const button = (event.target as Element).closest<HTMLButtonElement>("[data-globe-action]");
    if (!button) return;
    const action = button.dataset.globeAction;
    if (action === "reset") resetView();
    else setZoom(camera.zoom * (action === "in" ? 1.32 : 1 / 1.32));
    scheduleMotion();
  });
  const observer = new ResizeObserver(resize);
  observer.observe(canvas);
  window.addEventListener("scroll", clearHover, {passive: true});
  window.addEventListener("blur", clearHover);
  window.addEventListener("pagehide", clearHover);
  resize();
  return {
    select(country: CountryShape | undefined, center: boolean) {
      if (country !== selectedCountry || center) clearHover();
      selectedCountry = country;
      if (country && center) {
        const [longitude, latitude] = country.properties.center;
        rotate(-longitude, -latitude);
        if (country.properties.small && camera.zoom < 2.2) setZoom(2.2);
      }
      schedule();
      if (country && center && size.width && size.height) {
        finishDraw();
        const point = painter.projection(country.properties.center);
        if (point) showLabel(country, point, true);
      }
    },
    snapshot: () => ({...camera}),
    selected: () => selectedCountry,
    dispose() {
      observer.disconnect();
      window.removeEventListener("scroll", clearHover);
      window.removeEventListener("blur", clearHover);
      window.removeEventListener("pagehide", clearHover);
      clearHover();
      clearTimeout(settleTimer);
      cancelAnimationFrame(frame);
    }
  };
}
function mountGlobeBridge(stage: HTMLElement, globe: ReturnType<typeof mountGlobe>, hero: HTMLElement, canvas: HTMLCanvasElement) {
  const context = canvas.getContext("2d");
  if (!context) return () => {};
  const background = hero.querySelector<HTMLElement>(".bg")!;
  const section = stage.closest<HTMLElement>(".market-section")!;
  const title = section.querySelector("h2")!;
  const motion = matchMedia("(prefers-reduced-motion: reduce)");
  const painter = createGlobePainter(context);
  const clamp = (value: number) => Math.max(0, Math.min(1, value));
  const mix = (a: number, b: number, amount: number) => a + (b - a) * amount;
  function ease(from: number, to: number, progress: number) {
    const t = clamp((progress - from) / (to - from));
    return t * t * (3 - 2 * t);
  }

  let frame = 0;
  let cover: {vertical: number; radius: (width: number, height: number) => number; yaw: number; pitch: number} | undefined;
  let engaged = false;
  let interacting = false;
  const heroVars = ["--cover-shift-x", "--cover-shift-y", "--cover-scale", "--cover-globe-y"];
  const coverFadeVars = ["--cover-opacity", "--cover-copy-opacity", "--cover-controls-visibility"];
  function clearCoverFade() {
    coverFadeVars.forEach((property) => hero.style.removeProperty(property));
  }
  function clear() {
    if (!engaged) return;
    engaged = false;
    canvas.hidden = true;
    heroVars.forEach((property) => hero.style.removeProperty(property));
    stage.style.removeProperty("--dock-globe-opacity");
    section.style.removeProperty("--market-reveal");
    section.style.removeProperty("--market-frame-reveal");
  }
  function draw() {
    frame = 0;
    if (motion.matches || hero.hidden) {
      clear();
      clearCoverFade();
      return;
    }
    const heroRect = hero.getBoundingClientRect();
    const height = heroRect.height;
    const offset = parseFloat(getComputedStyle(title).scrollMarginTop) || 0;
    const pastCover = -heroRect.top;
    // End just before the global-market anchor comes to rest beneath the navigation.
    const duration = section.getBoundingClientRect().top + pastCover - offset;
    const start = height * .025;
    const end = Math.max(height * .70, duration - height * .024);
    const progress = clamp((pastCover - start) / (end - start));
    const dissolve = ease(.07, .44, progress);
    const copyOpacity = 1 - ease(.04, .38, progress);
    if (progress === 0) clearCoverFade();
    else {
      // Keep the cover faded until it leaves the viewport, including after docking.
      hero.style.setProperty("--cover-opacity", String(1 - dissolve));
      hero.style.setProperty("--cover-copy-opacity", String(copyOpacity));
      hero.style.setProperty("--cover-controls-visibility", copyOpacity ? "visible" : "hidden");
    }
    if (progress === 0 || progress === 1) {
      cover = undefined;
      interacting = false;
      clear();
      return;
    }
    if (interacting) return;

    const stageRect = stage.getBoundingClientRect();
    const width = document.documentElement.clientWidth;
    const mediaWidth = background.offsetWidth;
    const mediaHeight = background.offsetHeight;
    if (!cover) {
      // The video uses a low, oversized globe. The offline canvas has its own framing.
      cover = background.classList.contains("media-ready")
        ? {vertical: .914, radius: (w) => w * .455, yaw: -28, pitch: -10}
        : {vertical: 453 / 900, radius: (_w, h) => h * (382 / 900), yaw: -24, pitch: -12};
    }
    const fromX = heroRect.left + heroRect.width / 2;
    const fromY = heroRect.top + height / 2 + mediaHeight * (cover.vertical - .5);
    const fromRadius = cover.radius(mediaWidth, mediaHeight);
    const toX = stageRect.left + stageRect.width / 2;
    const toY = stageRect.top + stageRect.height / 2;
    const camera = globe.snapshot();
    const travel = ease(0, .91, progress);
    const x = mix(fromX, toX, travel);
    const y = mix(fromY, toY, travel);
    const baseRadius = globeRadius(stageRect.width, stageRect.height);
    const travelRadius = mix(fromRadius, baseRadius * Math.min(camera.zoom, 1.12), travel);
    const zoomIn = ease(.80, .91, progress);
    const radius = mix(travelRadius, baseRadius * camera.zoom, zoomIn);
    const dock = ease(.91, .98, progress);
    const reveal = ease(.68, .95, progress);
    const frameReveal = ease(.79, .97, progress);

    engaged = true;
    hero.style.setProperty("--cover-shift-x", `${x - fromX}px`);
    hero.style.setProperty("--cover-shift-y", `${y - fromY}px`);
    hero.style.setProperty("--cover-scale", String(radius / fromRadius));
    hero.style.setProperty("--cover-globe-y", `${cover.vertical * 100}%`);
    stage.style.setProperty("--dock-globe-opacity", String(dock));
    section.style.setProperty("--market-reveal", String(reveal));
    section.style.setProperty("--market-frame-reveal", String(frameReveal));
    canvas.hidden = false;
    canvas.style.opacity = String(dissolve * (1 - ease(.96, 1, progress)));

    const pixelRatio = Math.min(devicePixelRatio, 2);
    const pixelWidth = Math.round(width * pixelRatio), pixelHeight = Math.round(height * pixelRatio);
    if (canvas.width !== pixelWidth) canvas.width = pixelWidth;
    if (canvas.height !== pixelHeight) canvas.height = pixelHeight;
    const turn = ease(.10, .87, progress);
    const yawDelta = (camera.yaw - cover.yaw + 540) % 360 - 180;
    const orbit = {yaw: cover.yaw + yawDelta * turn, pitch: mix(cover.pitch, camera.pitch, turn),
      zoom: mix(1, camera.zoom, zoomIn)};
    const crop = ease(.79, .91, progress);
    const clip = crop > 0 ? {
      x: mix(-14, stageRect.left, crop),
      y: mix(-14, stageRect.top, crop),
      width: mix(width + 28, stageRect.width, crop),
      height: mix(height + 28, stageRect.height, crop)
    } : undefined;
    // The docked globe takes over the full-detail rendering as the bridge fades out.
    if (dissolve > 0) painter.draw({width, height, pixelRatio, x, y, radius, camera: orbit, clip,
      detail: ease(.24, .85, progress), moving: true,
      selectedCountry: progress >= .85 ? globe.selected() : undefined});
  }
  function schedule() {
    if (!frame) frame = requestAnimationFrame(draw);
  }
  function finishForInput(event: Event) {
    if (!engaged || !(event.target as Element).closest("canvas,.globe-hit,button,input,a")) return;
    interacting = true;
    clear();
  }

  const observer = new ResizeObserver(schedule);
  observer.observe(hero);
  observer.observe(stage);
  window.addEventListener("scroll", schedule, {passive: true});
  motion.addEventListener("change", schedule);
  section.addEventListener("focusin", finishForInput);
  section.addEventListener("pointerdown", finishForInput);
  schedule();
  return () => {
    observer.disconnect();
    window.removeEventListener("scroll", schedule);
    motion.removeEventListener("change", schedule);
    section.removeEventListener("focusin", finishForInput);
    section.removeEventListener("pointerdown", finishForInput);
    cancelAnimationFrame(frame);
    clear();
    clearCoverFade();
  };
}

export function mountAtlas(canvas: HTMLCanvasElement, onSelect: (code: string) => void,
                           hero: HTMLElement, bridge: HTMLCanvasElement) {
  const context = canvas.getContext('2d');
  if (!context) return;
  const globe = mountGlobe(canvas, context, onSelect);
  const stop = mountGlobeBridge(canvas.parentElement!, globe, hero, bridge);
  return {
    choose(code: string, center: boolean) {
      globe.select(countriesByCode.get(code), center);
    },
    dispose() {
      stop();
      globe.dispose();
    },
  };
}

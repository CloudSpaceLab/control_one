import { geoNaturalEarth1, geoPath } from 'd3-geo';
import { feature } from 'topojson-client';
import worldAtlas from 'world-atlas/countries-110m.json';

const atlas = worldAtlas as any;
const countries = (feature(atlas, atlas.objects.countries) as any).features.filter(
  (country: any) => String(country.id ?? '') !== '010',
);

const projection = geoNaturalEarth1().fitExtent(
  [
    [18, 18],
    [982, 462],
  ],
  { type: 'FeatureCollection', features: countries } as any,
);

const path = geoPath(projection);

export const WORLD_COUNTRY_PATHS = countries
  .map((country: any, index: number) => ({
    id: String(country.id ?? index),
    d: path(country as any) ?? '',
  }))
  .filter((country: { d: string }) => country.d.length > 0);

export function projectGeoCoordinates(lon: number, lat: number): { x: number; y: number } {
  const point = projection([lon, lat]);
  if (!point) {
    return { x: 500, y: 240 };
  }
  return { x: point[0], y: point[1] };
}

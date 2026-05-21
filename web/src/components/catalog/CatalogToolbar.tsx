import { useEffect, useState } from "react";
import { Search } from "lucide-react";

import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { CatalogFilters } from "@/lib/types";

export type CatalogToolbarValue = {
  query: string;
  mediaType: "all" | "movie" | "series" | "episode";
  libraryId: string;
  genre: string;
  yearBucket: string;
  sort: string;
  desc: boolean;
};

type Props = {
  value: CatalogToolbarValue;
  onChange: (next: CatalogToolbarValue) => void;
  filters?: CatalogFilters;
  libraries: Array<{ libraryId: string; libraryName: string }>;
};

// Sentinel used in place of "" for "no filter selected" options.
// @radix-ui/react-select forbids empty-string SelectItem values at
// runtime (it reserves "" to mean "clear / show placeholder"), so the
// previous `value=""` items crashed the toolbar on render. We use this
// sentinel internally and translate to/from "" at the Select boundary
// so the parent / API layer keeps its plain-string contract.
const NONE = "__none__";
const fromNone = (v: string): string => (v === NONE ? "" : v);
const toNone = (v: string): string => (v === "" ? NONE : v);

// CatalogToolbar is the sticky search + filter row above the grid.
// All state lives in the parent; this is a controlled component so
// debouncing / URL syncing can happen one level up.
export function CatalogToolbar({ value, onChange, filters, libraries }: Props) {
  // Local input state for the search field so typing doesn't trigger
  // a request per keystroke — the parent debounces value.query.
  const [search, setSearch] = useState(value.query);
  useEffect(() => {
    setSearch(value.query);
  }, [value.query]);

  return (
    <div className="sticky top-0 z-20 -mx-4 mb-6 grid grid-cols-1 gap-2 border-b border-border/60 bg-background/90 px-4 py-3 backdrop-blur-md sm:grid-cols-2 lg:grid-cols-7">
      <div className="lg:col-span-2 space-y-1.5">
        <Label htmlFor="catalog-search" className="sr-only">
          Search
        </Label>
        <div className="relative">
          <Search className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            id="catalog-search"
            className="pl-8"
            placeholder="Search titles, episodes…"
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              onChange({ ...value, query: e.target.value });
            }}
          />
        </div>
      </div>

      <Select value={value.mediaType} onValueChange={(v) => onChange({ ...value, mediaType: v as CatalogToolbarValue["mediaType"] })}>
        <SelectTrigger><SelectValue placeholder="Type" /></SelectTrigger>
        <SelectContent>
          <SelectItem value="all">All types</SelectItem>
          <SelectItem value="movie">Movies</SelectItem>
          <SelectItem value="series">Series</SelectItem>
          <SelectItem value="episode">Episodes</SelectItem>
        </SelectContent>
      </Select>

      <Select
        value={toNone(value.libraryId)}
        onValueChange={(v) => onChange({ ...value, libraryId: fromNone(v) })}
      >
        <SelectTrigger><SelectValue placeholder="Library" /></SelectTrigger>
        <SelectContent>
          <SelectItem value={NONE}>All libraries</SelectItem>
          {libraries.map((lib) => (
            <SelectItem key={lib.libraryId} value={lib.libraryId}>
              {lib.libraryName}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Select
        value={toNone(value.genre)}
        onValueChange={(v) => onChange({ ...value, genre: fromNone(v) })}
      >
        <SelectTrigger><SelectValue placeholder="Genre" /></SelectTrigger>
        <SelectContent>
          <SelectItem value={NONE}>All genres</SelectItem>
          {(filters?.genres ?? []).map((g) => (
            <SelectItem key={g} value={g}>
              {g}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Select
        value={toNone(value.yearBucket)}
        onValueChange={(v) => onChange({ ...value, yearBucket: fromNone(v) })}
      >
        <SelectTrigger><SelectValue placeholder="Decade" /></SelectTrigger>
        <SelectContent>
          <SelectItem value={NONE}>Any year</SelectItem>
          {(filters?.decades ?? []).map((d) => (
            <SelectItem key={d.label} value={`${d.yearMin}-${d.yearMax}`}>
              {d.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Select value={`${value.sort}|${value.desc ? "desc" : "asc"}`} onValueChange={(v) => {
        const [sort, dir] = v.split("|");
        onChange({ ...value, sort, desc: dir === "desc" });
      }}>
        <SelectTrigger><SelectValue placeholder="Sort" /></SelectTrigger>
        <SelectContent>
          <SelectItem value="title|asc">Title A–Z</SelectItem>
          <SelectItem value="title|desc">Title Z–A</SelectItem>
          <SelectItem value="year|desc">Newest first</SelectItem>
          <SelectItem value="year|asc">Oldest first</SelectItem>
          <SelectItem value="added_at|desc">Recently added</SelectItem>
          <SelectItem value="rating|desc">Highest rated</SelectItem>
        </SelectContent>
      </Select>
    </div>
  );
}

<?xml version="1.0" encoding="UTF-8"?>
<!--
 Tiny 16: Basic — the "things" sheet, by Lanea Zimmerman (Sharm), CC-BY 4.0.
 See CREDITS.md.

 A second tileset, so the palette has more than one section and the map's gids
 span two ranges. Tiled addresses tilesets by range and picks the highest first
 gid at or below an id, which is a rule with nothing to exercise it while a map
 declares one tileset.
-->
<tileset version="1.10" tiledversion="1.11.0" name="tiny16-things" tilewidth="16" tileheight="16" tilecount="96" columns="12">
  <properties><property name="entityType" value="Floor"/><property name="Passability.kind" value="open"/><property name="Visibility.kind" value="clear"/></properties>
 <image source="tiny16-things.png" width="192" height="128"/>
</tileset>

<?xml version="1.0" encoding="UTF-8"?>
<!--
 Tiny 16: Basic, by Lanea Zimmerman (Sharm), CC-BY 4.0. See CREDITS.md.

 A real sheet rather than a generated pair of squares: 8 columns of 15 rows at
 16px, so the browser suite exercises the row-and-column arithmetic in
 Tileset.SourceRect that a two-tile placeholder never reaches.

 passable is this engine's own tileset convention (Epic 14 Story 2), and it is
 what comp_tile.passable and therefore pathfinding read. Only the tiles the
 fixture maps actually use declare it; a tile that says nothing is passable, by
 the same rule.
-->
<tileset version="1.10" tiledversion="1.11.0" name="tiny16-basic" tilewidth="16" tileheight="16" tilecount="120" columns="8">
 <image source="tiny16-basic.png" width="128" height="240"/>
 <tile id="0" type="wall">
  <properties><property name="passable" type="bool" value="false"/></properties>
 </tile>
 <tile id="11" type="grass">
  <properties><property name="passable" type="bool" value="true"/></properties>
 </tile>
 <tile id="13" type="water">
  <properties><property name="passable" type="bool" value="false"/></properties>
 </tile>
 <tile id="14" type="path">
  <properties><property name="passable" type="bool" value="true"/></properties>
 </tile>
</tileset>

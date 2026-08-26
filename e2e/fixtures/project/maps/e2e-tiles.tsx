<?xml version="1.0" encoding="UTF-8"?>
<!--
 Two tiles at 24px: a floor you can walk on and a wall you cannot. passable is
 this engine's own tileset convention (Epic 14 Story 2), and it is what
 comp_tile.passable and therefore pathfinding read.
-->
<tileset version="1.10" tiledversion="1.11.0" name="e2e-tiles" tilewidth="24" tileheight="24" tilecount="2" columns="2">
 <image source="e2e-tiles.png" width="48" height="24"/>
 <tile id="0" type="floor">
  <properties><property name="passable" type="bool" value="true"/></properties>
 </tile>
 <tile id="1" type="wall">
  <properties><property name="passable" type="bool" value="false"/></properties>
 </tile>
</tileset>

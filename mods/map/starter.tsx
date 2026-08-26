<?xml version="1.0" encoding="UTF-8"?>
<!--
 The starter tileset: one floor tile and one wall tile, 32px square, in the two
 greys the renderer filled rectangles with before it could draw a tileset.

 passable is this engine's own convention, not Tiled's — internal/tiled names it
 in one place so the file that declares it and the loader that reads it cannot
 drift. A tile's class becomes comp_tile.tile_type.

 starter.png is generated: go run ./scripts/starter-tileset
-->
<tileset version="1.10" tiledversion="1.11.0" name="starter" tilewidth="32" tileheight="32" tilecount="2" columns="2">
 <image source="starter.png" width="64" height="32"/>
 <tile id="0" type="floor">
  <properties>
   <property name="passable" type="bool" value="true"/>
  </properties>
 </tile>
 <tile id="1" type="wall">
  <properties>
   <property name="passable" type="bool" value="false"/>
  </properties>
 </tile>
</tileset>

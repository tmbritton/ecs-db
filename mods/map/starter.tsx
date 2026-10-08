<?xml version="1.0" encoding="UTF-8"?>
<!--
 The starter tileset: one floor tile and one wall tile, 32px square, in the two
 greys the renderer filled rectangles with before it could draw a tileset.

  A tile's class becomes comp_tile.tile_type; movement and sight are authored
  on separate occupant entities, not on this artwork.

 starter.png is generated: go run ./scripts/starter-tileset
-->
<tileset version="1.10" tiledversion="1.11.0" name="starter" tilewidth="32" tileheight="32" tilecount="2" columns="2">
 <image source="starter.png" width="64" height="32"/>
  <tile id="0" type="floor"><properties>
   <property name="entityType" value="Floor"/>
   <property name="Passability.kind" value="open"/>
   <property name="Visibility.kind" value="clear"/>
  </properties></tile>
  <tile id="1" type="wall"><properties>
   <property name="entityType" value="Wall"/>
   <property name="Passability.kind" value="solid"/>
   <property name="Visibility.kind" value="opaque"/>
  </properties></tile>
</tileset>
